# Use the official Golang image to create a build artifact.
FROM golang:1.21 as builder

# Set the working directory inside the container.
WORKDIR /app

# Copy go mod and sum files.
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed.
RUN go mod download

# Copy the source code into the container.
COPY . .

# Build the Go app.
RUN CGO_ENABLED=0 GOOS=linux go build -v -o main .

# Start a new stage from scratch for a smaller image.
FROM alpine:latest

# Set the working directory.
WORKDIR /root/

# Copy the Pre-built binary file from the previous stage.
COPY --from=builder /app/main .

# Expose port 8086 to the outside world.
EXPOSE 8086

# Command to run the executable.
CMD ["./main"]