// Tenant extraction via auth package (avoids import cycle at package init).
package tenant

import (
	"opsmesh/pkg/auth"
)

// authExtractTenant validates a JWT token and returns the tenant_id claim.
func authExtractTenant(tokenString, secret string) (string, error) {
	claims, err := auth.ValidateServiceToken(tokenString, secret)
	if err != nil {
		return "", err
	}
	if claims.TenantID == "" {
		return "", ErrTenantNotFound
	}
	return claims.TenantID, nil
}
