package arthas

import (
	"context"
	"fmt"

	"k8s.io/client-go/rest"
)

const (
	// SideloadedJDKPath is where we drop the downloaded Temurin JDK inside
	// the target pod. Lives only as long as the pod does (intentionally;
	// a pod restart means re-download, which is fine for debug workflows).
	SideloadedJDKPath = "/tmp/jdk"

	// JDKArchivePath is the tarball staging location inside the pod.
	JDKArchivePath = "/tmp/jdk.tar.gz"
)

// adoptiumURL returns the redirect endpoint for a Linux/x64 Temurin HotSpot
// JDK of the given major version. The server 302s to the actual tarball.
func adoptiumURL(major int) string {
	return fmt.Sprintf(
		"https://api.adoptium.net/v3/binary/latest/%d/ga/linux/x64/jdk/hotspot/normal/eclipse",
		major,
	)
}

// ensureJDK downloads a full JDK of the requested major version into the pod
// at /tmp/jdk when the existing runtime lacks tools.jar. Returns the path to
// use as JAVA_HOME (empty string when no side-load was needed).
//
// Idempotent: if /tmp/jdk/lib/tools.jar already exists we skip the download.
// Fails loud (no silent fallback) when curl/wget are missing, the Adoptium
// download fails, or the extracted tree lacks tools.jar (CW-2).
func ensureJDK(ctx context.Context, restCfg *rest.Config, opts StartOptions, info JavaInfo) (string, error) {
	if !info.NeedsJDK() {
		return "", nil
	}
	script := fmt.Sprintf(`set -e
JDK_DIR=%[1]s
ARCHIVE=%[2]s
URL=%[3]q

if [ -f "$JDK_DIR/lib/tools.jar" ]; then
  exit 0
fi

# Fetch the tarball. Adoptium returns 302 -> CDN; curl -L follows.
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$URL" -o "$ARCHIVE"
elif command -v wget >/dev/null 2>&1; then
  wget -q "$URL" -O "$ARCHIVE"
else
  echo "neither curl nor wget available; cannot download JDK" >&2
  exit 127
fi

# Extract into a scratch dir then move into the stable path so partial
# extractions don't leave $JDK_DIR half-populated.
rm -rf "$JDK_DIR" /tmp/jdk-extract
mkdir -p /tmp/jdk-extract
if ! tar xzf "$ARCHIVE" -C /tmp/jdk-extract; then
  echo "failed to extract JDK tarball $ARCHIVE" >&2
  exit 1
fi

# Adoptium tarballs unpack to a single directory like jdk8u412-b08.
EXTRACTED=$(ls -1d /tmp/jdk-extract/*/ 2>/dev/null | head -n1)
if [ -z "$EXTRACTED" ]; then
  echo "JDK archive did not contain a top-level directory" >&2
  exit 1
fi
mv "$EXTRACTED" "$JDK_DIR"
rm -rf /tmp/jdk-extract "$ARCHIVE"

if [ ! -f "$JDK_DIR/lib/tools.jar" ]; then
  echo "downloaded JDK at $JDK_DIR is missing lib/tools.jar" >&2
  exit 1
fi
echo "JDK provisioned at $JDK_DIR"
`, SideloadedJDKPath, JDKArchivePath, adoptiumURL(info.Major))
	if err := execSh(ctx, restCfg, opts, script); err != nil {
		return "", fmt.Errorf("ensure JDK: %w", err)
	}
	return SideloadedJDKPath, nil
}
