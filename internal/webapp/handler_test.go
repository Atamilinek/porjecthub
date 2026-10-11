package webapp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHandlerServesAssetsAndSPAPathsButNotUnknownAPIRoutes(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    {Data: []byte("<html>hub</html>")},
		"assets/app.js": {Data: []byte("console.log('hub')")},
	}
	handler := NewHandler(assets)
	cases := []struct {
		path, want string
		status     int
	}{
		{"/", "<html>hub</html>", http.StatusOK},
		{"/projects/one", "<html>hub</html>", http.StatusOK},
		{"/assets/app.js", "console.log('hub')", http.StatusOK},
		{"/assets/missing.js", "404 page not found\n", http.StatusNotFound},
		{"/api/v1/missing", "404 page not found\n", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.status || w.Body.String() != tc.want {
				t.Fatalf("response status=%d body=%q, want %d %q", w.Code, w.Body.String(), tc.status, tc.want)
			}
		})
	}
}
