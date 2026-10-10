package c1

func validTag(tag string) bool {
	return len(tag) > len("proxy-") && tag[:len("proxy-")] == "proxy-" && len(tag) <= 128
}
