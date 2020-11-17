TAG=`git tag -l|tail -1`
COMMIT=`git rev-list -1 HEAD`
echo "Building TAG=$TAG COMMIT=$COMMIT"
docker build -t miko/ghatt .
docker tag miko/ghatt miko/ghatt:${TAG}


