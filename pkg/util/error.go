package util

import "fmt"

func WrapError(errPrefix string, err error) error {
	return fmt.Errorf(errPrefix+" - err msg: %s", err.Error())
}
