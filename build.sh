TAG=v1.2.24
docker build --build-arg=TAG=${TAG} -t miko/ghatt .
docker tag miko/ghatt miko/ghatt:${TAG}
docker build --build-arg=TAG=${TAG} --no-cache --rm -f Dockerfile.base -t miko/ghatt:base .
docker tag miko/ghatt:base miko/ghatt:${TAG}-base
echo git push miko/ghatt:${TAG}
echo git push miko/ghatt:latest
echo git push miko/ghatt:base
echo git push miko/ghatt:${TAG}-base

