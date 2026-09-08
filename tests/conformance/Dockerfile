# Official Ruby base pinned to the multi-platform image index.
FROM ruby:3.3.9-bookworm@sha256:9bf63765d224b035214b4e72170ba89dc6ca4cfbbbf151bf12e1c50e4ecb0e25
ARG LINGUIST_VERSION=9.7.0
ARG LINGUIST_SHA256=39a5e077e946029ef0ddbdc1344a53802455f4920f8c758d2a6b114d963d7734
RUN apt-get update && apt-get install -y --no-install-recommends cmake pkg-config libicu-dev time python3 && rm -rf /var/lib/apt/lists/*
RUN gem install --no-document charlock_holmes:0.7.9 mini_mime:1.1.5 rugged:1.9.6
RUN curl -fsSL "https://rubygems.org/downloads/github-linguist-${LINGUIST_VERSION}.gem" -o /tmp/linguist.gem \
    && echo "${LINGUIST_SHA256}  /tmp/linguist.gem" | sha256sum -c - \
    && gem install --no-document --ignore-dependencies /tmp/linguist.gem \
    && rm /tmp/linguist.gem \
    && github-linguist --version \
    && gem list --local > /reference-gems.txt
RUN git config --system --add safe.directory '*'
WORKDIR /workspace
