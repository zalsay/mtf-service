package services

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}
