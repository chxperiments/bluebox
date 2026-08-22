# Sample SAA stack, pointed at Floci (a local AWS emulator on port 4566).
# Copy into /data and run there so terraform state persists:
#
#   cp main.tf /data && cd /data
#   terraform init
#   terraform apply
#
# Covers the core SAA building blocks: a VPC with a public subnet and an
# internet gateway, a security group, an EC2 instance, and an S3 bucket.
# All are services Floci emulates.

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

variable "endpoint" {
  description = "Floci endpoint. Use the host's bridge address if Floci runs on the host."
  type        = string
  default     = "http://host.containers.internal:4566"
}

provider "aws" {
  region     = "us-east-1"
  access_key = "test"
  secret_key = "test"

  # Floci needs no real account, so skip the checks that would call out to
  # AWS, and use path-style S3 URLs against the local endpoint.
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  s3_use_path_style           = true

  endpoints {
    ec2 = var.endpoint
    s3  = var.endpoint
  }
}

resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  tags                 = { Name = "saa-vpc" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.1.0/24"
  map_public_ip_on_launch = true
  availability_zone       = "us-east-1a"
  tags                    = { Name = "saa-public" }
}

resource "aws_internet_gateway" "gw" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "saa-igw" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.gw.id
  }

  tags = { Name = "saa-public-rt" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

resource "aws_security_group" "web" {
  name        = "saa-web"
  description = "Allow HTTP from anywhere and SSH from within the VPC"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "HTTP"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/16"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "saa-web" }
}

resource "aws_instance" "web" {
  ami                    = "ami-0c02fb55956c7d316"
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.web.id]
  tags                   = { Name = "saa-web" }
}

resource "aws_s3_bucket" "assets" {
  bucket = "saa-assets-demo"
  tags   = { Name = "saa-assets" }
}

output "vpc_id" {
  value = aws_vpc.main.id
}

output "bucket" {
  value = aws_s3_bucket.assets.bucket
}
