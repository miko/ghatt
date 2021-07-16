TAG=v1.2.18
docker build --build-arg=TAG=${TAG} -t miko/ghatt .
docker tag miko/ghatt miko/ghatt:${TAG}
docker build --build-arg=TAG=${TAG} --no-cache --rm -f Dockerfile.base -t miko/ghatt:base .
docker tag miko/ghatt:base miko/ghatt:${TAG}-base


