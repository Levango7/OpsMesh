import re

path = r"F:\Agent\kilo\workspace\OpsMesh\internal\controlplane\kafka_ops.go"

with open(path, "r", encoding="utf-8") as f:
    content = f.read()

# 1. Add helper function after kafkaResourceRe line
helper = '''
// dispatchKafkaTask creates a system task for a kafka operation and returns its task ID.
func (s *Server) dispatchKafkaTask(w http.ResponseWriter, r *http.Request, deviceID, script, action string) (string, bool) {
\tactx, ok := s.requireTenantContext(w, r)
\tif !ok {
\t\treturn "", false
\t}
\ttask, err := s.createSystemTask(actx.TenantID, deviceID, script, "kafka_"+action)
\tif err != nil {
\t\tpaginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
\t\treturn "", false
\t}
\treturn task.TaskID, true
}
'''

marker = 'var kafkaResourceRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)'
assert marker in content, "marker not found"
content = content.replace(marker, marker + "\n" + helper, 1)

# 2 & 3. Replace dispatchTask calls
old_pattern = re.compile(
    r'(\t\t)taskID, err := s\.dispatchTask\(r, req\.DeviceID, script\)\n'
    r'(\t\t)if err != nil \{\n'
    r'(\t\t\t)paginate\.WriteJSON\(w, http\.StatusInternalServerError, map\[string\]string\{"error": err\.Error\(\)\}\)\n'
    r'(\t\t\t)return\n'
    r'(\t\t)\}\n'
    r'(\t\t)paginate\.WriteJSON\(w, http\.StatusAccepted, map\[string\]interface\{\}\{"taskID": taskID, "action": "([^"]+)"\}\)'
)

def replacer(m):
    indent = m.group(1)
    action = m.group(7)
    lines = [
        indent + 'taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "' + action + '")',
        indent + 'if !ok {',
        indent + '\treturn',
        indent + '}',
        indent + 'paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "' + action + '"})',
    ]
    return '\n'.join(lines)

new_content, count = old_pattern.subn(replacer, content)
print("Replacements made:", count)

with open(path, "w", encoding="utf-8") as f:
    f.write(new_content)

print("Done")
