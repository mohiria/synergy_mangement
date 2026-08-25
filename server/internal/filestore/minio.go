// Package filestore 封装交付物文件存取（ADR 0001：MinIO + 预签名 URL）。
package filestore

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Store 文件存取接口：预签名上传／下载与对象删除。
type Store interface {
	PresignPut(ctx context.Context, key string, expiry time.Duration) (string, error)
	PresignGet(ctx context.Context, key, downloadName string, expiry time.Duration) (string, error)
	Remove(ctx context.Context, key string) error
}

// Minio 基于 MinIO 的实现。presign 客户端可用对浏览器可达的公开地址签名
//（compose 内网地址与浏览器地址不同时经 MINIO_PUBLIC_ENDPOINT 区分）。
type Minio struct {
	client  *minio.Client
	presign *minio.Client
	bucket  string
}

func NewMinio(endpoint, publicEndpoint, accessKey, secretKey, bucket string, useSSL bool) (*Minio, error) {
	creds := credentials.NewStaticV4(accessKey, secretKey, "")
	// 固定 Region 跳过 bucket location 查询，预签名成为纯本地签名计算。
	opts := func() *minio.Options { return &minio.Options{Creds: creds, Secure: useSSL, Region: "us-east-1"} }
	client, err := minio.New(endpoint, opts())
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}
	presign := client
	if publicEndpoint != "" && publicEndpoint != endpoint {
		presign, err = minio.New(publicEndpoint, opts())
		if err != nil {
			return nil, fmt.Errorf("minio presign client: %w", err)
		}
	}
	return &Minio{client: client, presign: presign, bucket: bucket}, nil
}

// EnsureBucket 启动时确保桶存在（服务不可达时由调用方决定降级策略）。
func (m *Minio) EnsureBucket(ctx context.Context) error {
	exists, err := m.client.BucketExists(ctx, m.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return m.client.MakeBucket(ctx, m.bucket, minio.MakeBucketOptions{})
	}
	return nil
}

func (m *Minio) PresignPut(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := m.presign.PresignedPutObject(ctx, m.bucket, key, expiry)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (m *Minio) PresignGet(ctx context.Context, key, downloadName string, expiry time.Duration) (string, error) {
	params := url.Values{}
	if downloadName != "" {
		params.Set("response-content-disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
	}
	u, err := m.presign.PresignedGetObject(ctx, m.bucket, key, expiry, params)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (m *Minio) Remove(ctx context.Context, key string) error {
	return m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{})
}
