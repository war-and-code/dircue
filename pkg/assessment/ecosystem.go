package assessment

// EcosystemForManifest uses the same filename taxonomy as assessment project
// and candidate populations. Unknown filenames return "other".
func EcosystemForManifest(manifest string) string {
	_, ecosystem, _ := classifyManifest(manifest)
	return ecosystem
}
