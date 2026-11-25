#!/usr/bin/env bash
set -euo pipefail

echo "Waiting for Kafka brokers..."
for i in $(seq 1 60); do
  if kafka-broker-api-versions --bootstrap-server kafka-1:9092 >/dev/null 2>&1 && \
     kafka-broker-api-versions --bootstrap-server kafka-2:9092 >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

echo "Creating topic movie-events..."
kafka-topics --bootstrap-server kafka-1:9092,kafka-2:9092 \
  --create --if-not-exists \
  --topic movie-events \
  --partitions 3 \
  --replication-factor 2 \
  --config min.insync.replicas=1

kafka-topics --bootstrap-server kafka-1:9092 --describe --topic movie-events

echo "Waiting for Schema Registry..."
for i in $(seq 1 30); do
  if (echo > /dev/tcp/schema-registry/8081) >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

echo "Registering Avro schema in Schema Registry..."
ESCAPED_SCHEMA=$(tr -d '\n\r' < /schemas/movie_event.avsc | sed 's/\\/\\\\/g; s/"/\\"/g')
SCHEMA_PAYLOAD="{\"schema\":\"${ESCAPED_SCHEMA}\",\"schemaType\":\"AVRO\"}"

exec 3<>/dev/tcp/schema-registry/8081
printf "POST /subjects/movie-events-value/versions HTTP/1.1\r\nHost: schema-registry:8081\r\nContent-Type: application/vnd.schemaregistry.v1+json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s" "${#SCHEMA_PAYLOAD}" "${SCHEMA_PAYLOAD}" >&3
RESPONSE=$(cat <&3)
exec 3>&-

echo "Schema registered: ${RESPONSE}"
echo "Kafka & Schema Registry initialization complete."
