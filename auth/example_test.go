package auth_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/anatolykoptev/go-panel/auth"
)

// ExampleNewHMACAuth constructs the single-user HMAC authenticator and
// exercises its login page -- the simplest complete Authenticator a host can
// hand to resource.Config.Auth.
func ExampleNewHMACAuth() {
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("example-hmac-signing-key-32bytes"),
		BasePath: "/admin",
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	w := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(w, req)

	fmt.Println(w.Code)
	// Output: 200
}
