package lark

type tokenResponse struct {
	Code                  int    `json:"code"`
	Message               string `json:"msg"`
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
}

type providerEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Data    T      `json:"data"`
	Error   struct {
		LogID string `json:"log_id"`
	} `json:"error"`
}

type userInfo struct {
	OpenID       string `json:"open_id"`
	TenantKey    string `json:"tenant_key"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	AvatarURL    string `json:"avatar_url"`
	EmployeeType string `json:"employee_type"`
	Status       struct {
		IsActivated bool `json:"is_activated"`
		IsResigned  bool `json:"is_resigned"`
		IsFrozen    bool `json:"is_frozen"`
	} `json:"status"`
}

type searchData struct {
	Users     []userInfo `json:"users"`
	Items     []userInfo `json:"items"`
	HasMore   bool       `json:"has_more"`
	PageToken string     `json:"page_token"`
}

type tenantTokenResponse struct {
	Code              int    `json:"code"`
	Message           string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int64  `json:"expire"`
}

type messageData struct {
	MessageID string `json:"message_id"`
}
