#!/usr/bin/env python3
"""Kafka Producer 示例 — 轻量级，支持批量发送"""
import json
import time
import argparse
from kafka import KafkaProducer

def main():
    parser = argparse.ArgumentParser(description="Kafka Producer")
    parser.add_argument("--brokers", default="localhost:9092,localhost:9093,localhost:9094", help="Broker addresses")
    parser.add_argument("--topic", default="ops-events", help="Topic name")
    parser.add_argument("--count", type=int, default=10, help="Number of messages")
    parser.add_argument("--interval", type=float, default=0.5, help="Interval between messages (seconds)")
    parser.add_argument("--batch", type=int, default=1, help="Batch size")
    args = parser.parse_args()

    producer = KafkaProducer(
        bootstrap_servers=args.brokers.split(","),
        value_serializer=lambda v: json.dumps(v).encode("utf-8"),
        key_serializer=lambda k: k.encode("utf-8") if k else None,
        acks="all",
        retries=3,
        batch_size=16384,
        linger_ms=10,
    )

    print(f"Producing {args.count} messages to {args.topic}...")
    for i in range(args.count):
        msg = {
            "id": i + 1,
            "timestamp": time.time(),
            "event": "ops-event",
            "device": f"web-{i % 3 + 1:02d}",
            "metric": "cpu_usage",
            "value": round(50 + i * 3.5, 2),
        }
        key = f"device-{i % 3 + 1}"
        future = producer.send(args.topic, key=key, value=msg)
        if args.batch <= 1 or (i + 1) % args.batch == 0:
            future.get(timeout=10)
            print(f"  [{i+1}/{args.count}] sent: {msg}")
        time.sleep(args.interval)

    producer.flush()
    producer.close()
    print(f"Done. Sent {args.count} messages.")

if __name__ == "__main__":
    main()
