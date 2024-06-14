FROM golang:1.22

WORKDIR /app

COPY . .

RUN go mod tidy

RUN GOOS=linux GOARCH=amd64 go build -o main .

CMD ["./main"]