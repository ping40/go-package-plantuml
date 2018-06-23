package codeanalysis

import (
	"os"

	"fmt"

	log "github.com/Sirupsen/logrus"
)

func (this *analysisTool) filterUML(nodename string, nodedepth uint16, showtest bool) string {

	uml := ""
	var filteredStructMetas []*structMeta
	var newRelations []*DependencyRelation

	for _, structMeta1 := range this.structMetas {
		log.Infof("name: %s, package: %s", structMeta1.Name, structMeta1.baseInfo.PackagePath)
		if structMeta1.Name == nodename {
			if showtest || !structMeta1.isTest {
				filteredStructMetas = append(filteredStructMetas, structMeta1)
			}
		}
	}

	if len(filteredStructMetas) == 0 {
		log.Infof("找不到struct/interface: %s\n", nodename)
		os.Exit(-1)
	}

	showDependencyRelations(this.dependencyRelations)

	var addedStructMeta *structMeta
	var filteredDependencyRelations []*DependencyRelation
	var isNewRelation bool

	if len(filteredStructMetas) > 1 {
		for i := 0; i < len(filteredStructMetas)-1; i++ {
			for j := i + 1; j < len(filteredStructMetas); j++ {
				// 如果存在的关系，则把关系放到 filteredDependencyRelations
				if ok, r := isRelation(this.dependencyRelations, filteredStructMetas[i], filteredStructMetas[j]); ok {
					filteredDependencyRelations = append(filteredDependencyRelations, r)
				}
			}
		}
	}

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

			if showtest || !addedStructMeta.isTest {
				if exists := structExists(filteredStructMetas, newestStructMetas, addedStructMeta); !exists {

					addedStructMeta.Layer = layer
					newestStructMetas = append(newestStructMetas, addedStructMeta)

				}
				filteredDependencyRelations = append(filteredDependencyRelations, d)
			}
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
				if sm.scaned { // 表明这个接口的所有的实现类都已经加入到filteredStructMetas
					continue
				}

				if showtest || !sm.isTest {
					if this.inheritance(sm, structMeta1) {
						if newRelations, isNewRelation = checkNewRelation(newRelations, sm, structMeta1); isNewRelation {
							uml += structMeta1.implInterfaceUML(sm)
						}

						if exists := structExists(filteredStructMetas, newestStructMetas, sm); !exists {

							sm.Layer = layer
							newestStructMetas = append(newestStructMetas, sm)
						}
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

			impls := this.findInterfaceImpls(structMeta1)
			for _, impl := range impls {
				if showtest || !impl.isTest {
					if newRelations, isNewRelation = checkNewRelation(newRelations, structMeta1, impl); isNewRelation {
						uml += impl.implInterfaceUML(structMeta1)
					}
					if exists := structExists(filteredStructMetas, newestStructMetas, impl); !exists {

						impl.Layer = layer
						newestStructMetas = append(newestStructMetas, impl)
					}
				}
			}
		}

		filteredStructMetas = append(filteredStructMetas, newestStructMetas...)
	}

	for _, structMeta1 := range filteredStructMetas {
		uml += structMeta1.UML
		uml += "\n"
		uml += fmt.Sprintf("note top of %s: layer #%d%s \n", structMeta1.UniqueNameUML(), structMeta1.Layer, structMeta1.TextNote())
	}

	for _, d := range filteredDependencyRelations {
		uml += d.uml
		uml += "\n"
	}

	return "@startuml\n" + uml + "@enduml"
}

func checkNewRelation(relations []*DependencyRelation, source *structMeta, target *structMeta) ([]*DependencyRelation, bool) {
	for _, v := range relations {
		if v.source == source && v.target == target {
			return relations, false
		}
	}

	relations = append(relations, &DependencyRelation{
		source: source,
		target: target,
	})
	return relations, true
}

func isRelation(relations []*DependencyRelation, meta *structMeta, meta2 *structMeta) (bool, *DependencyRelation) {
	for _, r := range relations {
		if (isSame(r.source, meta) && isSame(r.target, meta2)) ||
			(isSame(r.source, meta2) && isSame(r.target, meta)) {
			return true, r
		}
	}
	return false, nil
}

func isSame(meta *structMeta, meta2 *structMeta) bool {
	if meta.Name == meta2.Name && meta.PackagePath == meta2.PackagePath {
		return true
	}
	return false
}

func showDependencyRelations(relations []*DependencyRelation) {
	log.Debug("dependency relation:")
	for _, r := range relations {
		log.Debug(r.uml)
	}
	log.Debug("dependency relation.  end")

}

func structExists(metas2 []*structMeta, metas []*structMeta, meta *structMeta) bool {
	for _, r := range metas {
		if meta.Name == r.Name && meta.PackagePath == r.PackagePath {
			return true
		}
	}
	for _, r := range metas2 {
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
	signs := definedInterface.MethodSigns
	if len(signs) < 1 {
		return false
	}

	checkedStruct := make([]*structMeta, 0, 10)
	for _, sign := range signs {
		if ok, _ := this.hasTheSign(impl, sign, checkedStruct); !ok {
			return false
		}
	}

	return true
}

func (this *analysisTool) hasTheSign(meta *structMeta, s string, checkedStruct []*structMeta) (bool, []*structMeta) {
	mySigns := meta.MethodSigns
	if sliceContains(mySigns, s) {
		return true, checkedStruct
	}
	checkedStruct = append(checkedStruct, meta)

	myParents := this.getMyParents(meta)

	var ok, exists bool
	for _, parent := range myParents {

		exists = false
		for _, v := range checkedStruct {
			if v == parent {
				exists = true
				break
			}
		}
		if exists {
			continue
		}

		if ok, checkedStruct = this.hasTheSign(parent, s, checkedStruct); ok {
			return true, checkedStruct
		}
	}
	return false, checkedStruct
}
