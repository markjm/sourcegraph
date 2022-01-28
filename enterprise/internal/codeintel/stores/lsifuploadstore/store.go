package lsifuploadstore

import (
	"context"

	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/internal/uploadstore"
)

func New(ctx context.Context, observationContext *observation.Context) (uploadstore.Store, error) {
	conf := &Config{}
	conf.Load()
	if err := conf.Validate(); err != nil {
		return nil, err
	}

	c := &uploadstore.Config{
		Backend:      conf.Backend,
		ManageBucket: conf.ManageBucket,
		Bucket:       conf.Bucket,
		TTL:          conf.TTL,
		S3: uploadstore.S3Config{
			Region:          conf.S3Region,
			Endpoint:        conf.S3Endpoint,
			AccessKeyID:     conf.S3AccessKeyID,
			SecretAccessKey: conf.S3SecretAccessKey,
			SessionToken:    conf.S3SessionToken,
		},
		GCS: uploadstore.GCSConfig{
			ProjectID:               conf.GCSProjectID,
			CredentialsFile:         conf.GCSCredentialsFile,
			CredentialsFileContents: conf.GCSCredentialsFileContents,
		},
	}

	uploadstore.CreateLazy(ctx, c, uploadstore.NewOperations(observationContext, "codeintel", "uploadstore"))
	return nil, nil
}
