package routermanifest

import "strings"

// DownloadURLs is transport policy, separate from the immutable artifact URL.
// Changing mirrors must not invalidate installed receipts or model capability
// fingerprints. Every source is checked against the same size and SHA256 pins.
func (a Artifact) DownloadURLs() []string {
	const llamaRelease = "https://github.com/ggml-org/llama.cpp/releases/download/b10828/"
	if strings.HasPrefix(a.URL, llamaRelease) {
		return []string{"https://gh-proxy.com/" + a.URL, a.URL}
	}
	const hfGGUF = "https://hf-mirror.com/XHToken/Spark-X2.6-4B-GGUF/resolve/" + spark26Rev + "/"
	const msGGUF = "https://modelscope.cn/models/XHToken/Spark-X2.6-4B-GGUF/resolve/8500d75a53f1e6592cfbb36fcb67c3e7380e6aab/"
	switch a.URL {
	case hfGGUF + "Spark-X2.6-4B-Q4_K_M.gguf", hfGGUF + "Spark-X2.6-4B-Q8_0.gguf":
		return []string{a.URL, msGGUF + strings.TrimPrefix(a.URL, hfGGUF)}
	case "https://hf-mirror.com/XHToken/Spark-X2.6-4B/resolve/" + spark26Base + "/chat_template.jinja":
		return []string{a.URL, "https://modelscope.cn/models/XHToken/Spark-X2.6-4B/resolve/1648980d7798c24041bedf58aff82615c562b6c6/chat_template.jinja"}
	default:
		return []string{a.URL}
	}
}
