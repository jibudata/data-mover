package datasource

// Describe the data from data repository, e.g., restic snapshot ID
type DataDescriptor struct {
	dataIdentifier string
	dataRepository DataRepository
}

func (d *DataDescriptor) GetDataIdentifier() string {
	// TBD
	return ""
}

func (d *DataDescriptor) GetDataDescriptorHandlers() interface{} {
	// TBD
	return nil
}
