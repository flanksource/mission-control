package utils

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
