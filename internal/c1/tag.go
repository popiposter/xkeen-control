package c1

func safeTag(tag string) string {
	if validTag(tag) {
		return tag
	}
	return ""
}
