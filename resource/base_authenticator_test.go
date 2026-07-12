package resource

import "github.com/anatolykoptev/go-panel/auth"

// Compile-time proof that the real Authenticator implementations satisfy the
// package's minimal baseAuthenticator contract — the exact shape Config.Auth
// and Panel.auth require (Require + LoginHandler + LogoutHandler), not the
// wider auth.Authenticator (which also demands Verified). If either type
// stops implementing one of these 3 methods, this file fails to compile.
var (
	_ baseAuthenticator = (*auth.HMACAuth)(nil)
	_ baseAuthenticator = (*auth.BcryptTOTPAuth)(nil)
)
