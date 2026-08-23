FROM mcr.microsoft.com/devcontainers/base:ubuntu24.04

RUN apt-get update && apt-get install -y postgresql-common \
    && yes | /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh \
    && apt-get install -y postgresql-18 postgresql-client-18 netcat-openbsd \
    && rm -rf /var/lib/apt/lists/*
