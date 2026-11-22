// db_bigdata_security_ops.go 大数据安全/权限运维（Kerberos/Ranger/Knox/LDAP/SSL）。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

var bigDataSecurityNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

var bigDataSecurityTypes = map[string]bool{
	"kerberos": true, "ranger": true, "knox": true, "ldap": true, "ssl": true,
}

var bigDataSecurityActions = map[string]bool{
	// Kerberos
	"kdc-status": true, "kdc-start": true, "kdc-stop": true,
	"principal-list": true, "principal-create": true, "principal-delete": true, "principal-modify": true,
	"keytab-generate": true, "keytab-list": true, "keytab-delete": true,
	"kinit-test": true, "klist": true,
	"realm-list": true, "realm-create": true, "realm-delete": true,
	"krb5-config": true, "krb5-check": true, "ticket-renew": true, "ticket-destroy": true,
	// Ranger
	"status": true, "policy-list": true, "policy-create": true, "policy-delete": true, "policy-update": true,
	"audit-log": true, "user-sync": true, "group-sync": true,
	"service-list": true, "service-create": true, "service-delete": true,
	"plugin-status": true, "plugin-enable": true, "plugin-disable": true,
	"tag-list": true, "tag-create": true, "tag-delete": true,
	"classification-list": true, "classification-create": true,
	// Knox
	"topology-list": true, "topology-create": true, "topology-delete": true,
	"service-add": true, "service-remove": true,
	"ldap-config": true, "ldap-test": true,
	"ssl-config": true, "ssl-cert-list": true, "ssl-cert-install": true, "gateway-restart": true,
	// LDAP/AD
	"ldap-status": true, "ldap-user-search": true, "ldap-group-search": true,
	"ad-domain-info": true, "ad-user-list": true, "ad-group-list": true,
	// SSL/TLS
	"ssl-cert-generate": true, "ssl-cert-verify": true, "ssl-config-show": true,
	"ssl-truststore-list": true, "ssl-keystore-list": true, "ssl-protocol-check": true,
}

func (s *Server) handleBigDataSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req map[string]interface{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deviceID := getStr(req, "deviceID")
	if deviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	sType := getStr(req, "type")
	if sType == "" {
		sType = "kerberos"
	}
	if !bigDataSecurityTypes[sType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + sType})
		return
	}
	action := getStr(req, "action")
	if action == "" || !bigDataSecurityNameRe.MatchString(action) || !bigDataSecurityActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !bigDataSecurityNameRe.MatchString(str) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + k})
			return
		}
	}

	actx, ok := s.subsystemAuthorize(w, r, "task:write")
	if !ok {
		return
	}
	if dev := s.store.Device(deviceID); dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	if ag := s.store.Agent(deviceID); ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	script := buildBigDataSecurityScript(sType, action, req)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "bigdata_security_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.bigdata.security", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_bigdata_security_" + action,
		Target: sType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + sType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}

func buildBigDataSecurityScript(sType, action string, req map[string]interface{}) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	principal := getStr(req, "principal")
	if principal == "" {
		principal = "user@EXAMPLE.COM"
	}
	name := getStr(req, "name")
	if name == "" {
		name = "default"
	}
	service := getStr(req, "service")
	if service == "" {
		service = "hdfs"
	}
	policy := getStr(req, "policy")
	if policy == "" {
		policy = "policy1"
	}
	realm := getStr(req, "realm")
	if realm == "" {
		realm = "EXAMPLE.COM"
	}
	topology := getStr(req, "topology")
	if topology == "" {
		topology = "default"
	}
	switch action {
	// Kerberos
	case "kdc-status":
		b.WriteString("systemctl status krb5kdc kadmin\n")
	case "kdc-start":
		b.WriteString("systemctl start krb5kdc kadmin\n")
	case "kdc-stop":
		b.WriteString("systemctl stop krb5kdc kadmin\n")
	case "principal-list":
		b.WriteString("kadmin.local -q 'listprincs'\n")
	case "principal-create":
		fmt.Fprintf(&b, "kadmin.local -q 'addprinc -randkey %s'\n", shellquote.Quote(principal))
	case "principal-delete":
		fmt.Fprintf(&b, "kadmin.local -q 'delprinc -force %s'\n", shellquote.Quote(principal))
	case "principal-modify":
		fmt.Fprintf(&b, "kadmin.local -q 'modprinc %s'\n", shellquote.Quote(principal))
	case "keytab-generate":
		fmt.Fprintf(&b, "kadmin.local -q 'ktadd -k /tmp/%s.keytab %s'\n", "service", shellquote.Quote(principal))
	case "keytab-list":
		b.WriteString("klist -kt /tmp/service.keytab\n")
	case "keytab-delete":
		b.WriteString("rm -f /tmp/service.keytab\n")
	case "kinit-test":
		fmt.Fprintf(&b, "kinit -kt /tmp/service.keytab %s && klist\n", shellquote.Quote(principal))
	case "klist":
		b.WriteString("klist\n")
	case "realm-list":
		b.WriteString("kadmin.local -q 'list_realms' 2>/dev/null || cat /etc/krb5.conf | grep realms -A5\n")
	case "realm-create":
		fmt.Fprintf(&b, "kdb5_util create -r %s -s\n", shellquote.Quote(realm))
	case "realm-delete":
		fmt.Fprintf(&b, "kdb5_util destroy -r %s -f\n", shellquote.Quote(realm))
	case "krb5-config":
		b.WriteString("cat /etc/krb5.conf\n")
	case "krb5-check":
		b.WriteString("krb5-config --all 2>/dev/null || kinit -V 2>&1 | head\n")
	case "ticket-renew":
		fmt.Fprintf(&b, "kinit -R %s 2>/dev/null; klist\n", shellquote.Quote(principal))
	case "ticket-destroy":
		b.WriteString("kdestroy\n")
	// Ranger
	case "status":
		b.WriteString("curl -s http://localhost:6080/service/public/v2/api/servicedefs\n")
	case "policy-list":
		b.WriteString("curl -s http://localhost:6080/service/public/v2/api/policy\n")
	case "policy-create":
		fmt.Fprintf(&b, "curl -s -X POST -H 'Content-Type: application/json' -d '{\"service\":\"%s\",\"name\":\"%s\",\"policyType\":0}' http://localhost:6080/service/public/v2/api/policy\n", service, policy)
	case "policy-delete":
		fmt.Fprintf(&b, "curl -s -X DELETE http://localhost:6080/service/public/v2/api/policy/%s\n", shellquote.Quote(policy))
	case "policy-update":
		fmt.Fprintf(&b, "curl -s -X PUT -H 'Content-Type: application/json' -d '{\"name\":\"%s\"}' http://localhost:6080/service/public/v2/api/policy/%s\n", policy, shellquote.Quote(policy))
	case "audit-log":
		b.WriteString("curl -s http://localhost:6080/service/public/v2/api/audit\n")
	case "user-sync":
		b.WriteString("curl -s -X POST http://localhost:6080/service/xusers/users\n")
	case "group-sync":
		b.WriteString("curl -s -X POST http://localhost:6080/service/xusers/groups\n")
	case "service-list":
		b.WriteString("curl -s http://localhost:6080/service/public/v2/api/servicedefs\n")
	case "service-create":
		fmt.Fprintf(&b, "curl -s -X POST -H 'Content-Type: application/json' -d '{\"name\":\"%s\",\"type\":\"hdfs\"}' http://localhost:6080/service/public/v2/api/service\n", service)
	case "service-delete":
		fmt.Fprintf(&b, "curl -s -X DELETE http://localhost:6080/service/public/v2/api/service/%s\n", shellquote.Quote(service))
	case "plugin-status":
		b.WriteString("curl -s http://localhost:6080/service/plugins/status\n")
	case "plugin-enable":
		b.WriteString("curl -s -X POST http://localhost:6080/service/plugins/enable\n")
	case "plugin-disable":
		b.WriteString("curl -s -X POST http://localhost:6080/service/plugins/disable\n")
	case "tag-list":
		b.WriteString("curl -s http://localhost:6080/service/tags\n")
	case "tag-create":
		fmt.Fprintf(&b, "curl -s -X POST -H 'Content-Type: application/json' -d '{\"name\":\"%s\"}' http://localhost:6080/service/tags\n", name)
	case "tag-delete":
		fmt.Fprintf(&b, "curl -s -X DELETE http://localhost:6080/service/tags/%s\n", shellquote.Quote(name))
	case "classification-list":
		b.WriteString("curl -s http://localhost:6080/service/classifications\n")
	case "classification-create":
		fmt.Fprintf(&b, "curl -s -X POST -H 'Content-Type: application/json' -d '{\"name\":\"%s\"}' http://localhost:6080/service/classifications\n", name)
	// Knox
	case "topology-list":
		b.WriteString("curl -s http://localhost:8443/gateway/admin/api/v1/topologies\n")
	case "topology-create":
		fmt.Fprintf(&b, "curl -s -X POST -H 'Content-Type: application/xml' -d '<topology name=\"%s\"></topology>' http://localhost:8443/gateway/admin/api/v1/topologies\n", topology)
	case "topology-delete":
		fmt.Fprintf(&b, "curl -s -X DELETE http://localhost:8443/gateway/admin/api/v1/topologies/%s\n", shellquote.Quote(topology))
	case "service-add":
		b.WriteString("curl -s -X POST http://localhost:8443/gateway/admin/api/v1/services\n")
	case "service-remove":
		b.WriteString("curl -s -X DELETE http://localhost:8443/gateway/admin/api/v1/services/default\n")
	case "ssl-config":
		b.WriteString("curl -s http://localhost:8443/gateway/admin/api/v1/ssl\n")
	case "ssl-cert-list":
		b.WriteString("keytool -list -keystore /etc/knox/keystore.jks\n")
	case "ssl-cert-install":
		b.WriteString("keytool -import -alias knox -file /tmp/cert.crt -keystore /etc/knox/keystore.jks\n")
	case "gateway-restart":
		b.WriteString("systemctl restart knox-gateway\n")
	// LDAP/AD
	case "ldap-status":
		b.WriteString("systemctl status slapd\n")
	case "ldap-config":
		b.WriteString("cat /etc/ldap/ldap.conf\n")
	case "ldap-test":
		b.WriteString("ldapsearch -x -b dc=example,dc=com\n")
	case "ldap-user-search":
		b.WriteString("ldapsearch -x '(objectClass=person)'\n")
	case "ldap-group-search":
		b.WriteString("ldapsearch -x '(objectClass=groupOfNames)'\n")
	case "ad-domain-info":
		b.WriteString("adcli info example.com\n")
	case "ad-user-list":
		b.WriteString("adcli list-users 2>/dev/null || echo 'ad users'\n")
	case "ad-group-list":
		b.WriteString("adcli list-groups 2>/dev/null || echo 'ad groups'\n")
	// SSL/TLS
	case "ssl-cert-generate":
		b.WriteString("keytool -genkeypair -alias server -keystore /tmp/keystore.jks\n")
	case "ssl-cert-verify":
		b.WriteString("keytool -printcert -file /tmp/cert.crt\n")
	case "ssl-config-show":
		b.WriteString("cat /etc/ssl/openssl.cnf 2>/dev/null || echo 'ssl config'\n")
	case "ssl-truststore-list":
		b.WriteString("keytool -list -keystore /etc/ssl/truststore.jks\n")
	case "ssl-keystore-list":
		b.WriteString("keytool -list -keystore /etc/ssl/keystore.jks\n")
	case "ssl-protocol-check":
		b.WriteString("openssl s_client -connect localhost:443 -tls1_2 </dev/null\n")
	default:
		fmt.Fprintf(&b, "echo 'bigdata security %s %s done'\n", sType, action)
	}
	fmt.Fprintf(&b, "echo 'bigdata security %s %s done'\n", sType, action)
	return b.String()
}
