# AWS Deployment (EC2 + S3)

This is a documented approach for a small deployment. It has not been run
against a live AWS account as part of this prototype.

## Components

| Piece | AWS service |
|---|---|
| API + worker containers | EC2 (t3.small, Amazon Linux 2023) running Docker Compose |
| Database | Amazon RDS for PostgreSQL (or the Postgres container for a demo) |
| Queue | RabbitMQ container on the same EC2 (or Amazon MQ for RabbitMQ) |
| Sample images, datasets, exported reports | S3 bucket `baggage-eval-samples` |
| Secrets | AWS Systems Manager Parameter Store (SecureString) |
| Logs | CloudWatch Logs via the CloudWatch agent (services log JSON to stdout) |

## Steps

1. **S3**: create the bucket, block public access, upload `images/` and `testdata/`.
   Baggage records point to images as `s3://baggage-eval-samples/images/BAG001.jpg`.
2. **IAM role for EC2**: allow `s3:GetObject`/`s3:PutObject` on the bucket and
   `ssm:GetParameter` on `/baggage-eval/*`. No access keys are stored on the server.
3. **Secrets**: store `DATABASE_URL` and `RABBITMQ_URL` in Parameter Store:
   ```bash
   aws ssm put-parameter --name /baggage-eval/DATABASE_URL --type SecureString --value 'postgres://...'
   ```
4. **EC2**: launch with the IAM role, install Docker and the compose plugin, clone the repo.
5. **Environment**: build `.env` from Parameter Store at boot:
   ```bash
   for k in DATABASE_URL RABBITMQ_URL; do
     echo "$k=$(aws ssm get-parameter --name /baggage-eval/$k --with-decryption --query Parameter.Value --output text)"
   done > .env
   ```
6. **Run**: `docker compose up -d --build api worker` (Postgres/RabbitMQ from managed services in production).
7. **Security groups**: open 443 (behind a load balancer or Nginx with TLS) to the API only;
   Postgres 5432 and RabbitMQ 5672 only from the EC2 security group.
8. **Health check**: `GET /healthz` for the load balancer target group.

## Configuration per environment

All settings are environment variables (see `.env.example`), so the same image
runs locally, in staging and in production with different values.
