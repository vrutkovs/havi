FROM registry.access.redhat.com/ubi8/ubi:latest as builder
WORKDIR /opt/app-root/src/
COPY . .
# copy git information for built crate
USER 0
RUN dnf update -y \
    && dnf install -y golang \
    && dnf clean all
RUN go build -o /tmp/build/havi .

FROM registry.access.redhat.com/ubi8/ubi:latest
COPY --from=builder /tmp/build/havi /usr/bin/
ENTRYPOINT ["/usr/bin/havi"]
