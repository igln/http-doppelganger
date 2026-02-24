package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

type URLRewriter struct {
	replacements []replacement
}

type replacement struct {
	old string
	new string
}

func NewURLRewriter(gitlabURLs []string, proxyURL string) *URLRewriter {
	var replacements []replacement

	for _, gitlabURL := range gitlabURLs {
		gitlabURL = strings.TrimSuffix(gitlabURL, "/")
		proxyURL = strings.TrimSuffix(proxyURL, "/")

		replacements = append(replacements, replacement{
			old: gitlabURL,
			new: proxyURL,
		})

		if strings.HasPrefix(gitlabURL, "https://") {
			httpVersion := "http://" + strings.TrimPrefix(gitlabURL, "https://")
			replacements = append(replacements, replacement{
				old: httpVersion,
				new: proxyURL,
			})
		} else if strings.HasPrefix(gitlabURL, "http://") {
			httpsVersion := "https://" + strings.TrimPrefix(gitlabURL, "http://")
			replacements = append(replacements, replacement{
				old: httpsVersion,
				new: proxyURL,
			})
		}
	}

	return &URLRewriter{
		replacements: replacements,
	}
}

func (r *URLRewriter) ShouldRewrite(resp *http.Response) bool {
	if len(r.replacements) == 0 {
		return false
	}

	contentType := resp.Header.Get("Content-Type")
	return strings.Contains(contentType, "text/html") ||
		strings.Contains(contentType, "application/json") ||
		strings.Contains(contentType, "application/javascript") ||
		strings.Contains(contentType, "text/javascript") ||
		strings.Contains(contentType, "text/css")
}

func (r *URLRewriter) RewriteResponse(resp *http.Response) error {
	if !r.ShouldRewrite(resp) {
		return nil
	}

	var reader io.ReadCloser
	var err error

	isGzipped := resp.Header.Get("Content-Encoding") == "gzip"
	if isGzipped {
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return nil
		}
		defer reader.Close()
	} else {
		reader = resp.Body
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	resp.Body.Close()

	modifiedBody := r.RewriteBody(body)

	if isGzipped {
		var buf bytes.Buffer
		gzWriter := gzip.NewWriter(&buf)
		gzWriter.Write(modifiedBody)
		gzWriter.Close()
		modifiedBody = buf.Bytes()
	}

	resp.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	resp.ContentLength = int64(len(modifiedBody))
	resp.Header.Set("Content-Length", "")
	resp.Header.Del("Content-Length")

	return nil
}

func (r *URLRewriter) RewriteBody(body []byte) []byte {
	result := body
	for _, rep := range r.replacements {
		result = bytes.ReplaceAll(result, []byte(rep.old), []byte(rep.new))
	}
	return result
}
