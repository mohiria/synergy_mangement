// Package filestore 封装交付物文件存取（ADR 0001：MinIO + 预签名 URL）。
package filestore

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Store 文件存取接口：预签名上传／下载、对象读取（成果包打包）与删除。
type Store interface {
	PresignPut(ctx context.Context, key string, expiry time.Duration) (string, error)
	PresignGet(ctx context.Context, key, downloadName string, inline bool, expiry time.Duration) (string, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (int64, error)
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Remove(ctx context.Context, key string) error
}

// Minio 基于 MinIO 的实现。presign 客户端可用对浏览器可达的公开地址签名
// （compose 内网地址与浏览器地址不同时经 MINIO_PUBLIC_ENDPOINT 区分）。
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

// PresignGet 预签名下载地址；inline 为真时按浏览器内联预览下发（#124），否则强制下载。
func (m *Minio) PresignGet(ctx context.Context, key, downloadName string, inline bool, expiry time.Duration) (string, error) {
	params := url.Values{}
	if downloadName != "" {
		disposition := "attachment"
		if inline {
			disposition = "inline"
		}
		params.Set("response-content-disposition", fmt.Sprintf("%s; filename=%q", disposition, downloadName))
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

// Get 读取对象内容（成果包整包下载用；调用方负责 Close）。
func (m *Minio) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// GetObject 懒连接：Stat 一次确认对象可读。
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, err
	}
	return obj, nil
}

// Stat 返回对象大小；对象不存在时返回错误。用于上传两阶段提交的确认步骤：
// 预签名直传绕过服务端，只有回来 Stat 一次才知道文件是否真的写进了对象存储。
func (m *Minio) Stat(ctx context.Context, key string) (int64, error) {
	info, err := m.client.StatObject(ctx, m.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}

// Put 直接写入对象（服务端生成内容或测试种子用）。
func (m *Minio) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, r, size, minio.PutObjectOptions{})
	return err
}
