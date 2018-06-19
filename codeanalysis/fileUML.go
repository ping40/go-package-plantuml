package codeanalysis

import (
	"os"

	"fmt"

	log "github.com/Sirupsen/logrus"
)

func (this *analysisTool) filterUML(nodename string, nodedepth uint16) string {

	uml := ""
	var filteredStructMetas []*structMeta

	for _, structMeta1 := range this.structMetas {
		if structMeta1.Name == nodename {
			filteredStructMetas = append(filteredStructMetas, structMeta1)
		}
	}

	if len(filteredStructMetas) == 0 {
		log.Infof("找不到struct/interface: %s\n", nodename)
		os.Exit(-1)
	}

	showDependencyRelations(this.dependencyRelations)

	var addedStructMeta *structMeta
	var filteredDependencyRelations []*DependencyRelation
	var layer uint16 = 1
	for ; layer <= nodedepth; layer++ {
		newestStructMetas := make([]*structMeta, 0, len(this.dependencyRelations))
		//从关系中找到下一层节点
		for _, d := range this.dependencyRelations {
			if exists := dependencyRelationExists(filteredDependencyRelations, d); exists {
				continue
			}

			source, target := relationExists(filteredStructMetas, d)
			if source == target {
				continue
			}

			if source {
				addedStructMeta = d.target
			} else {
				addedStructMeta = d.source
			}
			if exists := structExists(newestStructMetas, addedStructMeta); !exists {
				addedStructMeta.Layer = layer
				newestStructMetas = append(newestStructMetas, addedStructMeta)
			}
			filteredDependencyRelations = append(filteredDependencyRelations, d)
		}

		//从filteredStructMetas找没有扫描过的struct，进而找他们的实现的接口
		for _, structMeta1 := range filteredStructMetas {

			if structMeta1.category != StructCategory {
				continue
			}
			if structMeta1.scaned {
				continue
			}
			structMeta1.scaned = true

			for _, sm := range this.structMetas {
				if sm.category != InterfaceCategory {
					continue
				}
				if exists := structExists(filteredStructMetas, sm); exists {
					continue
				}
				if this.inheritance(sm, structMeta1) {
					if exists := structExists(newestStructMetas, sm); !exists {
						sm.Layer = layer
						newestStructMetas = append(newestStructMetas, sm)
						uml += structMeta1.implInterfaceUML(sm)
					}
				}
			}
		}

		//从filteredStructMetas找没有扫描过的interface，进而找这个接口的实现类
		for _, structMeta1 := range filteredStructMetas {

			if structMeta1.category != InterfaceCategory {
				continue
			}
			if structMeta1.scaned {
				continue
			}
			structMeta1.scaned = true

			for _, sm := range this.structMetas {
				if sm.category != StructCategory {
					continue
				}
				if exists := structExists(filteredStructMetas, sm); exists {
					continue
				}
				if this.inheritance(structMeta1, sm) {
					if exists := structExists(newestStructMetas, sm); !exists {
						sm.Layer = layer
						newestStructMetas = append(newestStructMetas, sm)
						uml += sm.implInterfaceUML(structMeta1)
					}
				}
			}
		}

		filteredStructMetas = append(filteredStructMetas, newestStructMetas...)
	}

	for _, structMeta1 := range filteredStructMetas {
		uml += structMeta1.UML
		uml += "\n"
		uml += fmt.Sprintf("note top of %s: layper %d \n", structMeta1.UniqueNameUML(), structMeta1.Layer)
	}

	for _, d := range filteredDependencyRelations {
		uml += d.uml
		uml += "\n"
	}

	return "@startuml\n" + uml + "@enduml"
}

func showDependencyRelations(relations []*DependencyRelation) {
	log.Debug("dependency relation:")
	for _, r := range relations {
		log.Debug(r.uml)
	}
	log.Debug("dependency relation.  end")

}

func structExists(metas []*structMeta, meta *structMeta) bool {
	for _, r := range metas {
		if meta.Name == r.Name && meta.PackagePath == r.PackagePath {
			return true
		}
	}
	return false
}

func relationExists(metas []*structMeta, relation *DependencyRelation) (source bool, target bool) {
	for _, r := range metas {
		if relation.source.Name == r.Name && relation.source.PackagePath == r.PackagePath {
			source = true
		}

		if relation.target.Name == r.Name && relation.target.PackagePath == r.PackagePath {
			target = true
		}
	}

	return
}

func dependencyRelationExists(relations []*DependencyRelation, relation *DependencyRelation) bool {
	for _, r := range relations {
		if r == relation {
			return true
		}
	}
	return false
}

func (this *analysisTool) inheritance(definedInterface, impl *structMeta) bool {
	if sliceContainsSlice(definedInterface.MethodSigns, impl.MethodSigns) {
		return true
	}

	return false
}
