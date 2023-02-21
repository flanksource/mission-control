package utils

import "encoding/json"

func Dedup[T comparable](arr []T) []T {
	set := make(map[T]bool)
	retArr := []T{}
	for _, item := range arr {
		if _, value := set[item]; !value {
			set[item] = true
			retArr = append(retArr, item)
		}
	}
	return retArr
}

// MergeStructs merges two structs where patch is applied on top of base
func MergeStructs[T any](base, patch T) (T, error) {
	jb, err := json.Marshal(patch)
	if err != nil {
		return base, err
	}
	err = json.Unmarshal(jb, &base)
	if err != nil {
		return base, err
	}

	return base, nil
}
