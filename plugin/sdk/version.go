package sdk

// FormatVersion produces the canonical version string embedded in the gRPC
// manifest: "<version> built <buildDate>". Empty build dates are omitted.
func FormatVersion(version, buildDate string) string {
	if buildDate == "" {
		return version
	}
	return version + " built " + buildDate
}
