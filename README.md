# Report Service

This Go application generates CSV reports from data retrieved via the EMMA API. The application uses Docker for containerization and is designed to support various types of reports.

## Prerequisites

- Docker installed on your machine.
- EMMA API client credentials.

## Getting Started

### Environment Variables
Set the CREDENTIALS environment variable in the format:
```shell
export CREDENTIALS="clientId1:clientSecret1,clientId2:clientSecret2"
```
### Building and Running with Docker
1. Build the Docker image:
```shell
docker build -t emma-report-generator .
```
2. Run the Docker container:
```shell
docker run -p 8080:8080 -e CREDENTIALS="$CREDENTIALS" emma-report-generator
```
### Accessing the Service
Once the container is running, you can access the service at:
```shell
http://localhost:8080/v1/vm-reports
```
### API Endpoint
- GET /v1/vm-reports
  - Generates a VM CSV report and serves it as a downloadable file.

## License

This project is licensed under the MIT License - see the [LICENSE](https://github.com/emma-community/emma-report-generator/blob/main/LICENSE) file for details.