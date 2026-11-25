package api

import "strings"

func validHostLabel(s string) bool {
	if len(s) == 0 || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func (r ServerRequest) Validate() *Error {
	switch {
	case !validHostLabel(r.Name):
		return NewError(CodeInvalidRequest, "name must be an RFC 1123 hostname label")
	case r.ServerType == "":
		return NewError(CodeInvalidRequest, "server_type is required")
	case r.Location == "":
		return NewError(CodeInvalidRequest, "location is required")
	case r.Image == "":
		return NewError(CodeInvalidRequest, "image is required")
	case len(r.UserData) > MaxUserDataBytes:
		return NewError(CodeInvalidRequest, "user_data exceeds %d bytes", MaxUserDataBytes)
	}
	return validateTenantLabels(r.Labels)
}

func (p ServerPatch) Validate() *Error {
	if p.Name != nil && !validHostLabel(*p.Name) {
		return NewError(CodeInvalidRequest, "name must be an RFC 1123 hostname label")
	}
	if p.Image != nil && *p.Image == "" {
		return NewError(CodeInvalidRequest, "image must not be empty")
	}
	if p.Labels != nil {
		return validateTenantLabels(*p.Labels)
	}
	return nil
}

func validateTenantLabels(labels map[string]string) *Error {
	for key := range labels {
		if strings.HasPrefix(key, TenantLabelPrefix) {
			return NewError(CodeInvalidRequest, "label key %q uses the reserved prefix %q", key, TenantLabelPrefix)
		}
	}
	return nil
}
