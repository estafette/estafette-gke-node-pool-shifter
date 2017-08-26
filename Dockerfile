FROM scratch

MAINTAINER estafette.io

COPY ca-certificates.crt /etc/ssl/certs/
COPY estafette-gke-node-pool-shifter /

CMD ["./estafette-gke-node-pool-shifter"]
