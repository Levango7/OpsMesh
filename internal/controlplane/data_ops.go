// Package controlplane data_pipeline.go 实现数据管道/ETL 运维
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleDataPipeline 处理 POST /api/v1/data/pipeline
func (s *Server) handleDataPipeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy-airflow/deploy-spark/deploy-flink/deploy-dbt/deploy-debezium/status
		Mode     string `json:"mode"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "deploy-airflow":
		version := req.Version
		if version == "" {
			version = "2.8.0"
		}
		script := fmt.Sprintf(`pip install apache-airflow==%s 2>/dev/null && airflow db init && airflow users create --username admin --password admin --firstname Admin --lastname User --role Admin --email admin@example.com 2>/dev/null && echo "Airflow %s deployed"`, version, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "pipeline_deploy-airflow")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-airflow"})

	case "deploy-spark":
		version := req.Version
		if version == "" {
			version = "3.5.0"
		}
		script := fmt.Sprintf(`wget -q https://archive.apache.org/dist/spark/spark-%s/spark-%s-bin-hadoop3.tgz -O /tmp/spark.tgz && tar -xzf /tmp/spark.tgz -C /opt/ && ln -sfn /opt/spark-%s-bin-hadoop3 /opt/spark && echo "Spark %s deployed"`, version, version, version, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "pipeline_deploy-spark")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-spark"})

	case "deploy-flink":
		version := req.Version
		if version == "" {
			version = "1.18.0"
		}
		script := fmt.Sprintf(`wget -q https://archive.apache.org/dist/flink/flink-%s/flink-%s-bin-scala_2.12.tgz -O /tmp/flink.tgz && tar -xzf /tmp/flink.tgz -C /opt/ && ln -sfn /opt/flink-%s /opt/flink && echo "Flink %s deployed"`, version, version, version, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "pipeline_deploy-flink")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-flink"})

	case "deploy-dbt":
		script := `pip install dbt-core dbt-postgres 2>/dev/null && dbt --version && echo "dbt deployed"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "pipeline_deploy-dbt")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-dbt"})

	case "deploy-debezium":
		script := `docker run -d --name debezium -p 8083:8083 -e GROUP_ID=1 -e CONFIG_STORAGE_TOPIC=my_connect_configs -e OFFSET_STORAGE_TOPIC=my_connect_offsets -e STATUS_STORAGE_TOPIC=my_connect_statuses debezium/connect:latest 2>/dev/null && echo "Debezium deployed"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "pipeline_deploy-debezium")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-debezium"})

	case "status":
		script := `echo "=== Airflow ===" && airflow version 2>/dev/null || echo "Airflow not installed" && echo "=== Spark ===" && /opt/spark/bin/spark-submit --version 2>&1 | head -1 || echo "Spark not installed" && echo "=== Flink ===" && /opt/flink/bin/flink --version 2>/dev/null || echo "Flink not installed" && echo "=== dbt ===" && dbt --version 2>/dev/null || echo "dbt not installed"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "pipeline_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleDataLake 处理 POST /api/v1/data/lake
func (s *Server) handleDataLake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy-hive/deploy-iceberg/deploy-delta/status
		Mode     string `json:"mode"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "deploy-hive":
		version := req.Version
		if version == "" {
			version = "3.1.3"
		}
		script := fmt.Sprintf(`wget -q https://archive.apache.org/dist/hive/hive-%s/apache-hive-%s-bin.tar.gz -O /tmp/hive.tgz && tar -xzf /tmp/hive.tgz -C /opt/ && ln -sfn /opt/apache-hive-%s-bin /opt/hive && echo "Hive %s deployed"`, version, version, version, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "lake_deploy-hive")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-hive"})

	case "deploy-iceberg":
		script := `echo "Iceberg runtime JARs need to be added to Spark/Hive/Trino classpath" && echo "Download: https://iceberg.apache.org/releases/" && echo "Place iceberg-spark-runtime and iceberg-hive-runtime in /opt/spark/jars/" && echo "Iceberg catalog config in spark-defaults.conf"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "lake_deploy-iceberg")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-iceberg"})

	case "deploy-delta":
		script := `echo "Delta Lake runtime JARs need to be added to Spark classpath" && echo "Download: https://docs.delta.io/latest/releases.html" && echo "Place delta-spark in /opt/spark/jars/" && echo "Enable: spark.sql.extensions=io.delta.sql.DeltaSparkSessionExtension"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "lake_deploy-delta")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-delta"})

	case "status":
		script := `echo "=== Hive ===" && /opt/hive/bin/hive --version 2>/dev/null || echo "Hive not installed" && echo "=== Iceberg ===" && ls /opt/spark/jars/iceberg* 2>/dev/null || echo "Iceberg not installed" && echo "=== Delta ===" && ls /opt/spark/jars/delta* 2>/dev/null || echo "Delta not installed"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "lake_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleDataWarehouse 处理 POST /api/v1/data/warehouse
func (s *Server) handleDataWarehouse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy-starrocks/deploy-doris/deploy-clickhouse/status
		Mode     string `json:"mode"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "deploy-starrocks":
		version := req.Version
		if version == "" {
			version = "3.2.0"
		}
		script := fmt.Sprintf(`wget -q https://releases.starrocks.io/starrocks/StarRocks-%s.tar.gz -O /tmp/starrocks.tgz && tar -xzf /tmp/starrocks.tgz -C /opt/ && ln -sfn /opt/StarRocks-%s /opt/starrocks && echo "StarRocks %s deployed"`, version, version, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "warehouse_deploy-starrocks")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-starrocks"})

	case "deploy-doris":
		version := req.Version
		if version == "" {
			version = "2.1.0"
		}
		script := fmt.Sprintf(`wget -q https://archive.apache.org/dist/doris/%s/apache-doris-%s-bin-x86_64.tar.gz -O /tmp/doris.tgz && tar -xzf /tmp/doris.tgz -C /opt/ && ln -sfn /opt/apache-doris-%s-bin-x86_64 /opt/doris && echo "Doris %s deployed"`, version, version, version, version)
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "warehouse_deploy-doris")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-doris"})

	case "deploy-clickhouse":
		script := `apt-get install -y clickhouse-server clickhouse-client 2>/dev/null || yum install -y clickhouse-server clickhouse-client 2>/dev/null && echo "ClickHouse deployed"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "warehouse_deploy-clickhouse")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-clickhouse"})

	case "status":
		script := `echo "=== StarRocks ===" && /opt/starrocks/fe/bin/start_fe.sh --version 2>/dev/null || echo "StarRocks not installed" && echo "=== Doris ===" && /opt/doris/fe/bin/start_fe.sh --version 2>/dev/null || echo "Doris not installed" && echo "=== ClickHouse ===" && clickhouse-server --version 2>/dev/null || echo "ClickHouse not installed"`
		taskID, ok := s.dispatchFastDFSTask(w, r, req.DeviceID, script, "warehouse_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册
// ============================================================================

// registerDataRoutes 注册数据管道/数据湖/数据仓库路由
func (s *Server) registerDataRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/data/pipeline", s.handleDataPipeline)
	mux.HandleFunc("/api/v1/data/lake", s.handleDataLake)
	mux.HandleFunc("/api/v1/data/warehouse", s.handleDataWarehouse)
}
