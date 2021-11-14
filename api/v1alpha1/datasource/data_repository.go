package datasource

// Data repository, e.g., S3
type DataRepository struct {
	dataType     string // Typed const? velero
	dataProvider string
	dataHandler  interface{}
}

func (r *DataRepository) GetDataType() string {
	// TBD
	return ""
}

func (r *DataRepository) GetDataProvider() string {
	// TBD
	return ""
}

/* Do we still need the repo identifier? suppose velero backupstoragelocation should kept this information.
func (r *DataRepository) GetRepoIdentifier() string {
	return ""
}
*/

// Get a structure for specific handler, e.g., for s3 it will return
// a structure which has s3 specific info and API
func (r *DataRepository) GetDataRepositoryHandlers() interface{} {
	// TBD
	return nil
}
