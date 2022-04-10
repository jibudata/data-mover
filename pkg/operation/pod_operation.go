package operation

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	config "github.com/jibudata/data-mover/pkg/config"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	memory             = "memory"
	cpu                = "cpu"
	defaultMemory      = "128Mi"
	defaultCPU         = "100m"
	stagePodNamePrefix = "stage-"
)

func truncateName(name string) string {
	r := regexp.MustCompile(`(-+)`)
	name = r.ReplaceAllString(name, "-")
	name = strings.TrimRight(name, "-")
	if len(name) > 57 {
		name = name[:57]
	}
	return name
}

// StagePod - wrapper for stage pod, allowing to compare  two stage pods for equality
type StagePod struct {
	core.Pod
}

// StagePodList - a list of stage pods, with built-in stage pod deduplication
type StagePodList []StagePod

func (p StagePod) volumesContained(pod StagePod) bool {
	if p.Namespace != pod.Namespace {
		return false
	}
	for _, volume := range p.Spec.Volumes {
		found := false
		for _, targetVolume := range pod.Spec.Volumes {
			if reflect.DeepEqual(volume.VolumeSource, targetVolume.VolumeSource) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (l *StagePodList) contains(pod StagePod) bool {
	for _, srcPod := range *l {
		if pod.volumesContained(srcPod) {
			return true
		}
	}

	return false
}

func (l *StagePodList) merge(list ...StagePod) {
	for _, pod := range list {
		if !l.contains(pod) {
			*l = append(*l, pod)
		}
	}
}

func (o *Operation) getStagePodImage() string {

	var image string = config.StagePodImage
	podList := &core.PodList{}
	options := &k8sclient.ListOptions{
		Namespace: config.VeleroNamespace,
	}
	err := o.client.List(context.TODO(), podList, options)
	if err != nil {
		o.logger.Error(err, "failed to list deployment", "namespace", config.VeleroNamespace)
		return image
	}

	for _, pod := range podList.Items {
		if strings.Contains(pod.Name, config.PodNamePrefix) {
			image = pod.Spec.Containers[0].Image
			break
		}
	}
	if image != config.StagePodImage {
                o.logger.Info("get data mover pod image", "image", image)
		return image[:strings.LastIndex(image, "/")+1] + config.StagePodVersion
	}
	return image
}

// backup poc namespace using velero
func (o *Operation) BuildStagePod(backupNamespace string, wait bool, tempNs string) error {
	podList := &core.PodList{}
	options := &k8sclient.ListOptions{
		Namespace: backupNamespace,
	}
	err := o.client.List(context.TODO(), podList, options)
	if err != nil {
		o.logger.Error(err, "failed to list pods", "namespace", backupNamespace)
		return err
	}

	stagePodImage := o.getStagePodImage()
	o.logger.Info("get stage pod image", "image", stagePodImage)

	stagePods := o.BuildStagePods(&podList.Items, stagePodImage, tempNs)
	for _, stagePod := range stagePods {
		err := o.client.Create(context.TODO(), &stagePod.Pod)
		if err != nil {
			o.logger.Error(err, fmt.Sprintf("Failed to create pod %s", stagePod.Pod.Name))
			return err
		}
	}
	running := false
	options = &k8sclient.ListOptions{
		Namespace: tempNs,
	}
	for !running && wait {
		time.Sleep(time.Duration(5) * time.Second)
		podList = &core.PodList{}
		_ = o.client.List(context.TODO(), podList, options)
		running = true
		for _, pod := range podList.Items {
			o.logger.Info(fmt.Sprintf("Pod %s status %s", pod.Name, pod.Status.Phase))
			if pod.Status.Phase != "Running" {
				running = false
			}
		}
	}
	return nil
}

func (o *Operation) GetStagePodStatus(tempNs string) (bool, error) {
	var running = true
	podList, err := o.getPodList(tempNs)
	if err != nil {
		return false, err
	}

	for _, pod := range podList {
		o.logger.Info(fmt.Sprintf("Pod %s status %s", pod.Name, pod.Status.Phase))
		if pod.Status.Phase != core.PodRunning {
			running = false
			break
		}
	}
	return running, nil
}

func (o *Operation) GetStagePodState(tempNs string) core.PodPhase {
	state := core.PodRunning
	podList, err := o.getPodList(tempNs)
	if err != nil {
		return core.PodFailed
	}

	for _, pod := range podList {
		o.logger.Info(fmt.Sprintf("Pod %s status %s", pod.Name, pod.Status.Phase))
		if pod.Status.Phase == core.PodFailed || pod.Status.Phase == core.PodUnknown {
			state = core.PodFailed
			break
		}
		if pod.Status.Phase == core.PodPending {
			state = core.PodPending
		}
	}
	return state
}

func (o *Operation) getPodList(ns string) ([]core.Pod, error) {
	options := &k8sclient.ListOptions{
		Namespace: ns,
	}
	podList := &core.PodList{}
	err := o.client.List(context.TODO(), podList, options)
	return podList.Items, err

}

func (o *Operation) EnsureStagePodCleaned(ns string) (bool, error) {
	podList, err := o.getPodList(ns)
	if err != nil {
		return false, err
	}
	for _, pod := range podList {
		if strings.HasPrefix(pod.Name, stagePodNamePrefix) {
			return false, nil
		}
	}

	return true, nil
}

// BuildStagePods - creates a list of stage pods from a list of pods
func (o *Operation) BuildStagePods(podList *[]core.Pod, stagePodImage string, ns string) StagePodList {

	existingPods, _ := o.getPodList(ns)
	var existingPodMap = make(map[string]bool)
	if len(existingPods) > 0 {
		for _, pod := range existingPods {
			name := pod.Name[len(stagePodNamePrefix):]
			existingPodMap[name] = true
		}
	}

	stagePods := StagePodList{}
	for _, pod := range *podList {
		if existingPodMap[pod.Name] {
			continue
		}
		volumes := []core.Volume{}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			volumes = append(volumes, volume)
		}
		if len(volumes) == 0 {
			continue
		}
		podKey := k8sclient.ObjectKey{
			Name:      pod.GetName(),
			Namespace: ns,
		}
		o.logger.Info(fmt.Sprintf("build stage pod %s", pod.Name))
		stagePod := o.BuildStagePodFromPod(podKey, &pod, volumes, stagePodImage, ns)
		if stagePod != nil {
			stagePods.merge(*stagePod)
		}
	}
	return stagePods
}

// Build a stage pod based on existing pod.
func (o *Operation) BuildStagePodFromPod(ref k8sclient.ObjectKey, pod *core.Pod, pvcVolumes []core.Volume, stagePodImage string, namespace string) *StagePod {

	// Base pod.
	newPod := &StagePod{
		Pod: core.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      truncateName(stagePodNamePrefix + ref.Name),
			},
			Spec: core.PodSpec{
				Containers: []core.Container{},
				// NodeName:   pod.Spec.NodeName,
				Volumes: pvcVolumes,
			},
		},
	}

	inVolumes := func(mount core.VolumeMount) bool {
		for _, volume := range pvcVolumes {
			if volume.Name == mount.Name {
				return true
			}
		}
		return false
	}
	podMemory, _ := resource.ParseQuantity(defaultMemory)
	podCPU, _ := resource.ParseQuantity(defaultCPU)
	// Add containers.
	for i, container := range pod.Spec.Containers {
		volumeMounts := []core.VolumeMount{}
		for _, mount := range container.VolumeMounts {
			if inVolumes(mount) {
				volumeMounts = append(volumeMounts, mount)
			}
		}
		stageContainer := core.Container{
			Name:            "sleep-" + strconv.Itoa(i),
			Image:           stagePodImage,
			ImagePullPolicy: core.PullIfNotPresent,
			Command:         []string{"sleep"},
			Args:            []string{"infinity"},
			VolumeMounts:    volumeMounts,
			Resources: core.ResourceRequirements{
				Requests: core.ResourceList{
					memory: podMemory,
					cpu:    podCPU,
				},
				Limits: core.ResourceList{
					memory: podMemory,
					cpu:    podCPU,
				},
			},
		}

		newPod.Spec.Containers = append(newPod.Spec.Containers, stageContainer)
	}

	return newPod
}

// delete pod

func (o *Operation) IsStagePodDeleted(ns string) bool {
	var running = false
	podList := &core.PodList{}
	options := &k8sclient.ListOptions{
		Namespace: ns,
	}
	_ = o.client.List(context.TODO(), podList, options)
	for _, pod := range podList.Items {
		if strings.HasPrefix(pod.Name, stagePodNamePrefix) {
			running = true
			break
		}
	}
	return !running
}

func (o *Operation) SyncDeleteStagePod(ns string) error {
	o.EnsureStagePodDeleted(ns)
	var running = true
	for running {
		time.Sleep(time.Duration(5) * time.Second)
		running = o.IsStagePodDeleted(ns)
	}
	return nil
}

func (o *Operation) EnsureStagePodDeleted(ns string) error {
	podList := &core.PodList{}
	options := &k8sclient.ListOptions{
		Namespace: ns,
	}
	err := o.client.List(context.TODO(), podList, options)
	if err != nil {
		o.logger.Error(err, fmt.Sprintf("Failed to get pod list in namespace %s", ns))
		return err
	}
	for _, pod := range podList.Items {
		var name = pod.Name
		if strings.HasPrefix(name, stagePodNamePrefix) {
			err = o.client.Delete(context.TODO(), &pod)
			if err != nil {
				o.logger.Error(err, fmt.Sprintf("Failed to delete pod %s", name))
				return err
			}
			o.logger.Info(fmt.Sprintf("Deleted pod %s", name))
		}
	}
	return nil
}
