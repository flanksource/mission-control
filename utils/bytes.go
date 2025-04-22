package utils

func Tail(data []byte, size int) []byte {
	if len(data) <= size {
		return data
	}

	return data[len(data)-size:]
}
