TAG=v1.2.26
docker build --build-arg=TAG=${TAG} -t miko/ghatt .
docker tag miko/ghatt miko/ghatt:${TAG}
docker build --build-arg=TAG=${TAG} --no-cache --rm -f Dockerfile.base -t miko/ghatt:base .
docker tag miko/ghatt:base miko/ghatt:${TAG}-base
echo docker push miko/ghatt:${TAG}
echo docker push miko/ghatt:latest
echo docker push miko/ghatt:base
echo docker push miko/ghatt:${TAG}-base

