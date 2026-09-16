package auth

type DiscordUser struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	Discriminator string  `json:"discriminator"`
	GlobalName    *string `json:"global_name"`
	Avatar        *string `json:"avatar"`
	Banner        *string `json:"banner"`
	AccentColor   *int    `json:"accent_color"`
	Locale        string  `json:"locale"`
	MFAEnabled    bool    `json:"mfa_enabled"`
	PremiumType   int     `json:"premium_type"`
	PublicFlags   int     `json:"public_flags"`
	// Email/Verified only populate if you also requested the "email" scope
	Email    *string `json:"email,omitempty"`
	Verified *bool   `json:"verified,omitempty"`
}
