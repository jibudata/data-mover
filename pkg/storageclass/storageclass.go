package storageclass

import "encoding/json"

func CreateAnnotationFromMap(storageClassMaps map[string]string) (string, error) {
	jsonStr, err := json.Marshal(storageClassMaps)
	if err != nil {
		return "", err
	}
	return string(jsonStr), nil
}
