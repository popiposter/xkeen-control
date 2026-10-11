package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/nativequality"
)

// maintenanceSettingsService is the optional settings surface of the native
// quality service.
type maintenanceSettingsService interface {
	MaintenanceSettings() nativequality.Settings
	SetMaintenanceSettings(nativequality.Settings) (nativequality.Settings, error)
}

type maintenanceSettingsView struct {
	Settings nativequality.Settings `json:"settings"`
	Defaults nativequality.Settings `json:"defaults"`
	Bounds   map[string][2]int      `json:"bounds"`
}

var maintenanceSettingsFields = map[string]bool{
	"enabled": true, "window": true, "refreshSubscriptions": true, "review": true, "updateGeodata": true,
	"autoApply": true, "speedCandidates": true, "poolSize": true, "recovery": true, "report": true,
}

func maintenanceView(value nativequality.Settings) maintenanceSettingsView {
	return maintenanceSettingsView{Settings: value, Defaults: nativequality.DefaultSettings(), Bounds: map[string][2]int{
		"speedCandidates": {nativequality.MinSpeedCandidates, nativequality.MaxSpeedCandidates},
		"poolSize":        {nativequality.MinPoolSize, nativequality.MaxPoolSize},
	}}
}

// handleMaintenanceSettings reads (GET) or replaces (POST, every field
// required, nothing else accepted) the nightly maintenance settings.
func (s *Server) handleMaintenanceSettings(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	settings, available := s.nativeQuality.(maintenanceSettingsService)
	if s.nativeQuality == nil || !available {
		writeError(w, http.StatusServiceUnavailable, "maintenance settings unavailable")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "query parameters unsupported")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, maintenanceView(settings.MaintenanceSettings()))
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported media type")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	defer r.Body.Close()
	fields, err := transferFields(json.NewDecoder(r.Body), maintenanceSettingsFields)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid maintenance settings")
		return
	}
	// Re-decode the exact field set into the typed value: a wrong JSON type
	// is rejected rather than coerced.
	raw, _ := json.Marshal(fields)
	var value nativequality.Settings
	if json.Unmarshal(raw, &value) != nil {
		writeError(w, http.StatusBadRequest, "invalid maintenance settings")
		return
	}
	if !s.transferSessionStillActive(r, session) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	saved, err := settings.SetMaintenanceSettings(value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "maintenance settings rejected")
		return
	}
	writeJSON(w, http.StatusOK, maintenanceView(saved))
}
