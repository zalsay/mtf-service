package models

type UZIReportOpenTokenRequest struct {
	RelativePath string `json:"relative_path" binding:"required"`
}

type UZIReportOpenTokenResponse struct {
	OpenToken string `json:"open_token"`
	ExpiresIn int    `json:"expires_in"`
	OpenURL   string `json:"open_url"`
}
