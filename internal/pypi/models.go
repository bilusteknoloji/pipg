package pypi

// PackageInfo represents the top-level response from the PyPI JSON API.
// Endpoint: GET https://pypi.org/pypi/{package_name}/json
type PackageInfo struct {
	Info     Info             `json:"info"`
	URLs     []URL            `json:"urls"`
	Releases map[string][]URL `json:"releases"`
}

// Info contains package metadata from the PyPI API response.
type Info struct {
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	Summary        string            `json:"summary"`
	RequiresDist   []string          `json:"requires_dist"`
	RequiresPython string            `json:"requires_python"`
	PackageURL     string            `json:"package_url"`
	ProjectURL     string            `json:"project_url"`
	ProjectURLs    map[string]string `json:"project_urls"`
	Yanked         bool              `json:"yanked"`
	YankedReason   string            `json:"yanked_reason"`
}

// URL represents a downloadable file (wheel or sdist) from the PyPI API response.
type URL struct {
	Filename       string  `json:"filename"`
	URL            string  `json:"url"`
	Size           int64   `json:"size"`
	PackageType    string  `json:"packagetype"` // "bdist_wheel" or "sdist"
	PythonVersion  string  `json:"python_version"`
	RequiresPython string  `json:"requires_python"`
	Digests        Digests `json:"digests"`
	Yanked         bool    `json:"yanked"`
	YankedReason   string  `json:"yanked_reason"`
}

// Digests contains hash digests for verifying downloaded files.
type Digests struct {
	SHA256     string `json:"sha256"`
	MD5        string `json:"md5"`
	Blake2b256 string `json:"blake2b_256"`
}
