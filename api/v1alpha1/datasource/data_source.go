package datasource

import corev1 "k8s.io/api/core/v1"

// Source of the data, e.g., PV
type DataSource struct {
	dataSourceType      string
	dataSourceReference corev1.ObjectReference
	dataSourceHandlers  interface{}
}

func (s *DataSource) GetDataSourceType() string {
	// TBD
	return ""
}

func (s *DataSource) GetDataSourceReference() corev1.ObjectReference {
	// TBD
	return corev1.ObjectReference{}
}

// Return a structure for specific handler, which includes more details
// E.g., volumesnapshot, PV, database table are different data source
func (s *DataSource) GetDataSourceHandlers() interface{} {
	// TBD
	return nil
}
