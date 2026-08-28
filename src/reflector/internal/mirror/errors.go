package mirror

import "fmt"

func ErrUnexpectedObjectType(action string, obj interface{}) error {
	return fmt.Errorf("%s: unexpected object type %T", action, obj)
}

func cloneByteMap(source map[string][]byte) map[string][]byte {
	if source == nil {
		return nil
	}
	cloned := make(map[string][]byte, len(source))
	for key, value := range source {
		if value == nil {
			cloned[key] = nil
			continue
		}
		copied := make([]byte, len(value))
		copy(copied, value)
		cloned[key] = copied
	}
	return cloned
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
