FROM debian:buster-slim

# Install necessary packages
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        python3 \
        python3-pip \
        git \
        build-essential \
        default-libmysqlclient-dev \
        && rm -rf /var/lib/apt/lists/*

# Clone the MISP-Taxii-Server repository
WORKDIR /app
RUN git clone --recursive https://github.com/MISP/MISP-Taxii-Server

# Install Python dependencies
WORKDIR /app/MISP-Taxii-Server
RUN pip3 install -r REQUIREMENTS.txt

# Install the TAXII server
RUN python3 setup.py install

# Expose the TAXII server port
EXPOSE 9000

# Set environment variables
ENV OPENTAXII_CONFIG=/app/MISP-Taxii-Server/config.yaml
ENV PYTHONPATH=/app/MISP-Taxii-Server

# Create databases and grant permissions (adjust credentials as needed)
RUN echo "CREATE DATABASE taxiiauth; \
          CREATE DATABASE taxiipersist; \
          GRANT ALL ON taxiiauth.* TO 'taxii'@'%' IDENTIFIED BY 'some_password'; \
          GRANT ALL ON taxiipersist.* TO 'taxii'@'%' IDENTIFIED BY 'some_password';" > /tmp/db_setup.sql

# Copy configuration files
COPY config/config.default.yaml config/config.yaml
COPY config/services.yaml config/services.yaml
COPY config/collections.yaml config/collections.yaml

# Configure the TAXII server (adjust settings in config.yaml as needed)
RUN sed -i 's/db_connection: mysql:\/\/root:@localhost\/taxiiauth/db_connection: mysql:\/\/taxii:some_password@mysql\/taxiiauth/g' config/config.yaml
RUN sed -i 's/db_connection: mysql:\/\/root:@localhost\/taxiipersist/db_connection: mysql:\/\/taxii:some_password@mysql\/taxiipersist/g' config/config.yaml

# Create the SQL tables
RUN opentaxii-create-services -c config/services.yaml && \
    opentaxii-create-collections -c config/collections.yaml

# Start the TAXII server
CMD ["opentaxii-run-dev", "-c", "config/config.yaml"]
