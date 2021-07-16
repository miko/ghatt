TAG=v1.2.16
docker build -t miko/ghatt .
docker tag miko/ghatt miko/ghatt:${TAG}
docker build --no-cache --rm -f Dockerfile.base -t miko/ghatt:base .
docker tag miko/ghatt:base miko/ghatt:${TAG}-base


