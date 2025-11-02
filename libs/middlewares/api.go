package middlewares
import (
	"net/http"

	"github.com/gorilla/mux"
)

// DefaultApiHeaders sets the default API headers
func DefaultAPIHeader() mux.MiddlewareFunc {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("User-Agent", "Web Service")
			w.Header().Set("X-Powered-By", "INFRATEL")
			h.ServeHTTP(w, r)
		})
	}
}