package api

import "net/http"

func HandleIPv6Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "ipv6:read") {
		return
	}
	status := lxcManager.DetectIPv6Status()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: status})
}

func assignIPv6(w http.ResponseWriter, r *http.Request, id int) {
	c, err := assignIPv6ByRuntime(id)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "IPv6 assigned", Data: c})
}
