package s3presign

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
}

type Presigner struct {
	bucket string
	client *s3.PresignClient
}

func New(ctx context.Context, bucket string, cfg Config) (*Presigner, error) {
	if bucket == "" {
		return nil, fmt.Errorf("missing bucket")
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("missing region")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("missing s3 credentials")
	}

	creds := credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")

	awsCfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(creds),
		config.WithEndpointResolverWithOptions(endpointResolver(cfg.Endpoint)),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.UsePathStyle
	})

	return &Presigner{
		bucket: bucket,
		client: s3.NewPresignClient(s3Client),
	}, nil
}

func (p *Presigner) PresignPutObject(ctx context.Context, objectKey string, contentType string, expires time.Duration) (string, error) {
	if objectKey == "" {
		return "", fmt.Errorf("missing object key")
	}

	req, err := p.client.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(p.bucket),
		Key:         aws.String(objectKey),
		ContentType: aws.String(contentType),
	}, func(o *s3.PresignOptions) {
		o.Expires = expires
	})
	if err != nil {
		return "", fmt.Errorf("presign put object: %w", err)
	}

	return req.URL, nil
}

func endpointResolver(endpoint string) aws.EndpointResolverWithOptionsFunc {
	return aws.EndpointResolverWithOptionsFunc(func(service, region string, _ ...any) (aws.Endpoint, error) {
		if endpoint == "" {
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		}
		if service == s3.ServiceID {
			return aws.Endpoint{
				URL:               endpoint,
				PartitionID:       "aws",
				SigningRegion:     region,
				HostnameImmutable: true,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})
}
