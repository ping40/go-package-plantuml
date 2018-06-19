#!/bin/sh

C=go-package-plantuml
GOPATH=/gp
PROJECT=/gp/src/github.com/hyperledger/fabric-sdk-go/
OUTPUTDIR=/tmp/plantuml/

for name in FabricSDK Provider
do
    for x in 1 2
    do
        echo "$C --codedir $PROJECT --gopath $GOPATH --outputdir $OUTPUTDIR --nodename $name --nodedepth $x --ignoredir $PROJECT/internal --ignoredir $PROJECT/third_party"
        $C --codedir $PROJECT --gopath $GOPATH --outputdir $OUTPUTDIR --nodename $name --nodedepth $x --ignoredir $PROJECT/internal --ignoredir $PROJECT/third_party
        sed -i 's/github.com\\\\hyperledger\\\\//g' $OUTPUTDIR/node-$name-$x.puml
        java -Xmx2048m -jar plantuml.jar $OUTPUTDIR/node-$name-$x.puml -tsvg
    done
done

