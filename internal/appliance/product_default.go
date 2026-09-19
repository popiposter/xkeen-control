package appliance

import "errors"

// ProductDefault is the source-owned, deterministic policy authority used by
// fresh Setup Mode. It is deliberately reconstructed from embedded product
// inputs rather than from a router, a request body, or mutable state.
func ProductDefault() Appliance {
	value, err := productDefault()
	if err != nil {
		// These bytes are compiled into the panel. A malformed product default is
		// a developer/build error, not a runtime condition that can be repaired
		// from operator input.
		panic("invalid embedded product default")
	}
	return value
}

// ValidateProductDefault is used by focused fixtures and startup-independent
// qualification to keep the embedded default fail-closed.
func ValidateProductDefault() error {
	_, err := productDefault()
	return err
}

func productDefault() (Appliance, error) {
	dns, err := compatibilityTemplates.ReadFile("templates/02_dns.json")
	if err != nil {
		return Appliance{}, errors.New("embedded product DNS policy is unavailable")
	}
	routing, err := compatibilityTemplates.ReadFile("templates/05_routing.json")
	if err != nil {
		return Appliance{}, errors.New("embedded product routing policy is unavailable")
	}
	observatory, err := compatibilityTemplates.ReadFile("templates/07_observatory.json")
	if err != nil {
		return Appliance{}, errors.New("embedded product Observatory policy is unavailable")
	}
	return parseActivePolicyBytes(dns, routing, observatory)
}
