FROM kdfs-spdk:dev

EXPOSE 4420 4421

ENTRYPOINT ["/usr/local/bin/nvmf_tgt", "-r", "/var/tmp/spdk.sock"]
