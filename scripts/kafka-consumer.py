#!/usr/bin/env python3
"""Kafka Consumer 示例 — 轻量级，支持消费者组"""
import json
import argparse
from kafka import KafkaConsumer

def main():
    parser = argparse.ArgumentParser(description="Kafka Consumer")
    parser.add_argument("--brokers", default="localhost:9092,localhost:9093,localhost:9094", help="Broker addresses")
    parser.add_argument("--topic", default="ops-events", help="Topic name")
    parser.add_argument("--group", default="ops-consumer-group", help="Consumer group")
    parser.add_argument("--from-beginning", action="store_true", help="Read from beginning")
    parser.add_argument("--max", type=int, default=0, help="Max messages (0=unlimited)")
    args = parser.parse_args()

    consumer = KafkaConsumer(
        args.topic,
        bootstrap_servers=args.brokers.split(","),
        group_id=args.group,
        auto_offset_reset="earliest" if args.from_beginning else "latest",
        enable_auto_commit=True,
        value_deserializer=lambda m: (json.loads(m.decode("utf-8")) if m.startswith(b'{') else m.decode("utf-8")),
        key_deserializer=lambda k: k.decode("utf-8") if k else None,
    )

    print(f"Consuming from {args.topic} (group: {args.group})...")
    count = 0
    try:
        for msg in consumer:
            count += 1
            print(f"  [{count}] key={msg.key} partition={msg.partition} offset={msg.offset}")
            print(f"       value={json.dumps(msg.value, ensure_ascii=False)}")
            if args.max > 0 and count >= args.max:
                break
    except KeyboardInterrupt:
        print("\nInterrupted.")
    finally:
        consumer.close()
        print(f"Done. Consumed {count} messages.")

if __name__ == "__main__":
    main()
