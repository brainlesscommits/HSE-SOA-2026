package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"cinema-analytics/internal/aggregator"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Exporter struct {
	client *minio.Client
	bucket string
	pgRepo *aggregator.PostgresRepo
	logger *slog.Logger
}

func NewS3Exporter(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useSSL bool, pgRepo *aggregator.PostgresRepo, logger *slog.Logger) (*S3Exporter, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:       useSSL,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}

	exp := &S3Exporter{
		client: client,
		bucket: bucket,
		pgRepo: pgRepo,
		logger: logger,
	}

	for i := 0; i < 15; i++ {
		exists, err := client.BucketExists(ctx, bucket)
		if err == nil {
			if !exists {
				if mkErr := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); mkErr != nil {
					logger.Warn("failed to create S3 bucket, will retry", "bucket", bucket, "error", mkErr)
				} else {
					logger.Info("created S3 bucket", "bucket", bucket)
				}
			}
			return exp, nil
		}
		logger.Warn("waiting for S3 availability", "attempt", i+1, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	return exp, nil
}

func (e *S3Exporter) ExportDate(ctx context.Context, metricDate time.Time) (string, error) {
	dateStr := metricDate.Format("2006-01-02")
	objectKey := fmt.Sprintf("daily/%s/aggregates.json", dateStr)
	s3URI := fmt.Sprintf("s3://%s/%s", e.bucket, objectKey)

	report, err := e.pgRepo.GetDailyReport(ctx, metricDate)
	if err != nil {
		e.logger.Error("s3 export failed: unable to read from postgres",
			"metric_date", dateStr,
			"error", err,
		)
		return "", fmt.Errorf("read aggregates from postgres: %w", err)
	}

	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal export json: %w", err)
	}

	exists, err := e.client.BucketExists(ctx, e.bucket)
	if err != nil {
		e.logger.Error("s3 export failed: s3 unavailable", "bucket", e.bucket, "error", err)
		return "", fmt.Errorf("check s3 bucket: %w", err)
	}
	if !exists {
		if err := e.client.MakeBucket(ctx, e.bucket, minio.MakeBucketOptions{}); err != nil {
			return "", fmt.Errorf("create s3 bucket: %w", err)
		}
	}

	_, err = e.client.PutObject(
		ctx,
		e.bucket,
		objectKey,
		bytes.NewReader(payload),
		int64(len(payload)),
		minio.PutObjectOptions{ContentType: "application/json"},
	)
	if err != nil {
		e.logger.Error("s3 export failed: upload error", "uri", s3URI, "error", err)
		return "", fmt.Errorf("upload to s3 %s: %w", s3URI, err)
	}

	e.logger.Info("daily aggregates exported to S3",
		"uri", s3URI,
		"metric_date", dateStr,
		"metrics_count", len(report.Metrics),
		"top_movies_count", len(report.TopMovies),
		"bytes", len(payload),
	)
	return s3URI, nil
}
