// Package dataetl 提供 ETL 作业模型、内存存储与引擎适配器。
package dataetl

import (
	"fmt"
	"strings"
)

// adapters.go 各引擎的命令构建器。

// seaTunnelAdapter SeaTunnel 作业命令构建器。
type seaTunnelAdapter struct{}

func (seaTunnelAdapter) Name() string { return EngineSeaTunnel }

func (seaTunnelAdapter) BuildCommand(job EtlJob) (string, error) {
	if strings.TrimSpace(job.Source) == "" || strings.TrimSpace(job.Sink) == "" {
		return "", fmt.Errorf("%w: source and sink are required", ErrInvalid)
	}
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("seatunnel.sh --config /etc/seatunnel/" + safeName(job.Name) + ".conf\n")
	if job.Transform != "" {
		b.WriteString("# transform: " + job.Transform + "\n")
	}
	return b.String(), nil
}

// dataXAdapter DataX 作业命令构建器。
type dataXAdapter struct{}

func (dataXAdapter) Name() string { return EngineDataX }

func (dataXAdapter) BuildCommand(job EtlJob) (string, error) {
	if strings.TrimSpace(job.Source) == "" || strings.TrimSpace(job.Sink) == "" {
		return "", fmt.Errorf("%w: source and sink are required", ErrInvalid)
	}
	return fmt.Sprintf("python datax.py /etc/datax/%s.json\n", safeName(job.Name)), nil
}

// sqoopAdapter Sqoop 作业命令构建器。
type sqoopAdapter struct{}

func (sqoopAdapter) Name() string { return EngineSqoop }

func (sqoopAdapter) BuildCommand(job EtlJob) (string, error) {
	if strings.TrimSpace(job.Source) == "" || strings.TrimSpace(job.Sink) == "" {
		return "", fmt.Errorf("%w: source and sink are required", ErrInvalid)
	}
	return fmt.Sprintf("sqoop import --connect %s --table %s --target-dir %s\n", job.Source, job.Sink, job.Sink), nil
}

// sparkAdapter Spark 作业命令构建器。
type sparkAdapter struct{}

func (sparkAdapter) Name() string { return EngineSpark }

func (sparkAdapter) BuildCommand(job EtlJob) (string, error) {
	if strings.TrimSpace(job.Source) == "" {
		return "", fmt.Errorf("%w: source is required", ErrInvalid)
	}
	return fmt.Sprintf("spark-submit --master yarn --deploy-mode cluster --class %s %s\n", safeName(job.Name), job.Source), nil
}

// airflowAdapter Airflow DAG 触发命令构建器。
type airflowAdapter struct{}

func (airflowAdapter) Name() string { return EngineAirflow }

func (airflowAdapter) BuildCommand(job EtlJob) (string, error) {
	if strings.TrimSpace(job.Name) == "" {
		return "", fmt.Errorf("%w: name is required", ErrInvalid)
	}
	return fmt.Sprintf("airflow dags trigger %s\n", safeName(job.Name)), nil
}

// kafkaConnectAdapter Kafka Connect connector 创建命令构建器。
type kafkaConnectAdapter struct{}

func (kafkaConnectAdapter) Name() string { return EngineKafkaConn }

func (kafkaConnectAdapter) BuildCommand(job EtlJob) (string, error) {
	if strings.TrimSpace(job.Source) == "" || strings.TrimSpace(job.Sink) == "" {
		return "", fmt.Errorf("%w: source and sink are required", ErrInvalid)
	}
	return fmt.Sprintf("curl -s -X POST http://localhost:8083/connectors -H 'Content-Type: application/json' -d '{\"name\":\"%s\",\"config\":{\"connector.class\":\"io.debezium.connector.mysql.MySqlConnector\"}}'\n", safeName(job.Name)), nil
}

// flinkCDCAdapter Flink CDC 作业命令构建器。
type flinkCDCAdapter struct{}

func (flinkCDCAdapter) Name() string { return EngineFlinkCDC }

func (flinkCDCAdapter) BuildCommand(job EtlJob) (string, error) {
	if strings.TrimSpace(job.Source) == "" || strings.TrimSpace(job.Sink) == "" {
		return "", fmt.Errorf("%w: source and sink are required", ErrInvalid)
	}
	return fmt.Sprintf("flink run -c com.ververica.cdc.connectors.mysql.source.MySqlSource %s\n", job.Source), nil
}

// canalAdapter Canal 作业命令构建器。
type canalAdapter struct{}

func (canalAdapter) Name() string { return EngineCanal }

func (canalAdapter) BuildCommand(job EtlJob) (string, error) {
	if strings.TrimSpace(job.Source) == "" {
		return "", fmt.Errorf("%w: source is required", ErrInvalid)
	}
	return fmt.Sprintf("canal.sh start %s\n", safeName(job.Name)), nil
}

// AdapterFor 返回指定引擎的适配器。
func AdapterFor(engine string) (EngineAdapter, error) {
	switch engine {
	case EngineSeaTunnel:
		return seaTunnelAdapter{}, nil
	case EngineDataX:
		return dataXAdapter{}, nil
	case EngineSqoop:
		return sqoopAdapter{}, nil
	case EngineSpark:
		return sparkAdapter{}, nil
	case EngineAirflow:
		return airflowAdapter{}, nil
	case EngineKafkaConn:
		return kafkaConnectAdapter{}, nil
	case EngineFlinkCDC:
		return flinkCDCAdapter{}, nil
	case EngineCanal:
		return canalAdapter{}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported engine %q", ErrInvalid, engine)
	}
}

func safeName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "job"
	}
	return string(out)
}
