package codeanalysis

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	log "github.com/Sirupsen/logrus"
)

type Category int

const (
	UnkownCategory    Category = iota // value --> 0
	InterfaceCategory                 // value --> 1
	StructCategory                    // value --> 2
)

type Config struct {
	CodeDir         string
	GopathDir       string
	VendorDir       string
	IgnoreDirs      []string
	TestPartialDirs []string
	IgnoreNodes     []string
}

type AnalysisResult interface {
	OutputToFile(logdir string, nodename string, nodedepth uint16, showtest bool)
}

func AnalysisCode(config Config) AnalysisResult {
	tool := &analysisTool{
		structMetas:                 []*structMeta{},
		typeAliasMetas:              []*typeAliasMeta{},
		packagePathPackageNameCache: map[string]string{},
		dependencyRelations:         []*DependencyRelation{},
	}
	tool.analysis(config)
	return tool
}

func HasPrefixInSomeElement(value string, src []string, vendorDir string) bool {
	result := false
	for _, srcValue := range src {
		if strings.HasPrefix(value, srcValue) {
			result = true
			break
		}
	}

	if strings.HasPrefix(value, vendorDir) {
		result = true
	}
	return result
}

func sliceContains(src []string, value string) bool {
	isContain := false
	for _, srcValue := range src {
		if srcValue == value {
			isContain = true
			break
		}
	}
	return isContain
}

func mapContains(src map[string]string, key string) bool {
	if _, ok := src[key]; ok {
		return true
	}
	return false
}

func findGoPackageNameInDirPath(dirpath string) string {

	dir_list, e := ioutil.ReadDir(dirpath)

	if e != nil {
		fmt.Errorf("读取目录%s文件列表失败,%s", dirpath, e)
		return ""
	}

	for _, fileInfo := range dir_list {
		if !fileInfo.IsDir() && strings.HasSuffix(fileInfo.Name(), ".go") {
			packageName := ParsePackageNameFromGoFile(path.Join(dirpath, fileInfo.Name()))
			if packageName != "" {
				return packageName
			}
		}
	}

	return ""
}

func ParsePackageNameFromGoFile(filepath string) string {

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath, nil, parser.ParseComments)

	if err != nil {
		log.Errorf("解析文件%s失败, %s", filepath, err)
		return ""
	}

	return file.Name.Name

}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

func packagePathToUML(packagePath string) string {
	packagePath = strings.Replace(packagePath, "/", "\\\\", -1)
	packagePath = strings.Replace(packagePath, "-", "_", -1)
	return packagePath
}

type baseInfo struct {
	// go文件路径
	FilePath string
	// 包路径, 例如 git.oschina.net/jscode/list-interface
	PackagePath string
}

type structMeta struct {
	baseInfo
	Name string
	// 直接方法签名列表,不包括继承的,
	MethodSigns []string
	// UML图节点
	UML string
	// layer 信息
	Layer uint16
	// struct,interface记录
	category Category
	//是否已经赵过它的实现者，仅仅对接口有效
	scaned bool
	// 是否是测试类
	isTest bool
}

type typeAliasMeta struct {
	baseInfo
	Name           string
	targetTypeName string
}

func (this *structMeta) UniqueNameUML() string {
	return packagePathToUML(this.PackagePath) + "." + this.Name
}

func (this *structMeta) ColorfulUML() string {
	if this.isTest {
		return " #MediumSpringGreen"
	} else if this.category == StructCategory {
		return " #LightSkyBlue"
	}
	return " #LightCyan"
}

func (this *structMeta) TextNote() string {
	if this.isTest {
		return " TEST"
	}
	return ""
}

func (this *structMeta) needScan() bool {
	return this.category == InterfaceCategory && !this.scaned
}

func (this *structMeta) implInterfaceUML(interfaceMeta1 *structMeta) string {
	return fmt.Sprintf("%s <|.. %s\n", interfaceMeta1.UniqueNameUML(), this.UniqueNameUML())
}

type importMeta struct {
	// 例如 main
	Alias string
	// 例如 git.oschina.net/jscode/list-interface
	Path string
}

type DependencyRelation struct {
	source *structMeta
	target *structMeta
	uml    string
	// 依赖关系是否是继承关系
	inheritance bool
}

type analysisTool struct {
	config Config

	// 当前解析的go文件, 例如/appdev/go-demo/src/git.oschina.net/jscode/list-interface/a.go
	currentFile string
	// 当前解析的go文件,所在包路径, 例如git.oschina.net/jscode/list-interface
	currentPackagePath string
	// 当前解析的go文件,引入的其他包
	currentFileImports []*importMeta

	// 所有的struct
	structMetas []*structMeta
	// 所有的别名定义
	typeAliasMetas []*typeAliasMeta
	// package path与package name的映射关系,例如git.oschina.net/jscode/list-interface 对应的pakcage name为 main
	packagePathPackageNameCache map[string]string
	// struct之间的依赖关系
	dependencyRelations []*DependencyRelation
}

func (this *analysisTool) analysis(config Config) {

	this.config = config

	if this.config.CodeDir == "" || !PathExists(this.config.CodeDir) {
		log.Errorf("找不到代码目录%s\n", this.config.CodeDir)
		return
	}

	if this.config.GopathDir == "" || !PathExists(this.config.GopathDir) {
		log.Errorf("找不到GOPATH目录%s\n", this.config.GopathDir)
		return
	}

	for _, lib := range stdlibs {
		this.mapPackagePath_PackageName(lib, path.Base(lib))
	}

	dir_walk_once := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") || HasPrefixInSomeElement(path, config.IgnoreDirs, config.VendorDir) {
				return filepath.SkipDir
			}
		}

		if strings.HasSuffix(path, ".go") {
			isTest := this.checkIsTest(path)
			log.Info("解析1 " + path)
			this.visitTypeInFile(path, isTest)
		}

		return nil
	}

	filepath.Walk(config.CodeDir, dir_walk_once)

	dir_walk_twice := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") || HasPrefixInSomeElement(path, config.IgnoreDirs, config.VendorDir) {
				return filepath.SkipDir
			}
		}

		if strings.HasSuffix(path, ".go") {
			log.Info("解析2 " + path)
			this.visitFuncInFile(path)
		}
		return nil
	}

	filepath.Walk(config.CodeDir, dir_walk_twice)

}

func (this *analysisTool) checkIsTest(s string) bool {
	if strings.HasSuffix(s, "_test.go") {
		return true
	}

	for _, tpd := range this.config.TestPartialDirs {
		if strings.Contains(s, tpd) {
			return true
		}
	}
	return false
}

func (this *analysisTool) initFile(path string) {
	log.Debug("path=", path)

	this.currentFile = path
	this.currentPackagePath = this.filepathToPackagePath(path)

	if this.currentPackagePath == "" {
		log.Errorf("packagePath为空,currentFile=%s\n", this.currentFile)
	}

}

func (this *analysisTool) mapPackagePath_PackageName(packagePath string, packageName string) {
	if packagePath == "" || packageName == "" {
		log.Errorf("mapPackagePath_PackageName, packageName=%s, packagePath=%s\n, current_file=%s",
			packageName, packagePath, this.currentFile)
		return
	}

	if mapContains(this.packagePathPackageNameCache, packagePath) {
		return
	}

	log.Debugf("mapPackagePath_PackageName, packageName=%s, packagePath=%s\n", packageName, packagePath)
	this.packagePathPackageNameCache[packagePath] = packageName

}

func (this *analysisTool) visitTypeInFile(path string, isTest bool) {

	this.initFile(path)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)

	if err != nil {
		log.Fatal(err)
		return
	}

	this.mapPackagePath_PackageName(this.currentPackagePath, file.Name.Name)

	for _, decl := range file.Decls {

		genDecl, ok := decl.(*ast.GenDecl)

		if ok {
			for _, spec := range genDecl.Specs {

				typeSpec, ok := spec.(*ast.TypeSpec)

				if ok {
					if !this.inIgnoreNode(typeSpec.Name.Name) {
						this.visitTypeSpec(typeSpec, isTest)
					}
				}
			}
		}

	}

}

func (this *analysisTool) visitTypeSpec(typeSpec *ast.TypeSpec, isTest bool) {

	interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
	if ok {
		this.visitInterfaceType(typeSpec.Name.Name, interfaceType)
		return
	}

	structType, ok := typeSpec.Type.(*ast.StructType)
	if ok {
		this.visitStructType(typeSpec.Name.Name, structType, isTest)
		return
	}

	// 其他类型别名
	this.typeAliasMetas = append(this.typeAliasMetas, &typeAliasMeta{
		baseInfo: baseInfo{
			FilePath:    this.currentFile,
			PackagePath: this.currentPackagePath,
		},
		Name:           typeSpec.Name.Name,
		targetTypeName: "",
	})

}

func (this *analysisTool) filepathToPackagePath(filepath string) string {

	filepath = path.Dir(filepath)

	if this.config.VendorDir != "" {
		if strings.HasPrefix(filepath, this.config.VendorDir) {
			packagePath := strings.TrimPrefix(filepath, this.config.VendorDir)
			packagePath = strings.TrimPrefix(packagePath, "/")
			return packagePath
		}
	}

	if this.config.GopathDir != "" {
		srcdir := path.Join(this.config.GopathDir, "src")
		if strings.HasPrefix(filepath, srcdir) {
			packagePath := strings.TrimPrefix(filepath, srcdir)
			packagePath = strings.TrimPrefix(packagePath, "/")
			return packagePath
		}
	}

	log.Errorf("无法确认包路径名, filepath=%s\n", filepath)

	return ""

}

func (this *analysisTool) visitFuncInFile(path string) {

	this.initFile(path)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)

	if err != nil {
		log.Fatal(err)
		return
	}

	this.currentFileImports = []*importMeta{}

	if file.Imports != nil {
		for _, import1 := range file.Imports {

			alias := ""
			packagePath := strings.TrimSuffix(strings.TrimPrefix(import1.Path.Value, "\""), "\"")

			if import1.Name != nil {
				alias = import1.Name.Name
			} else {
				aliasCache, ok := this.packagePathPackageNameCache[packagePath]
				log.Debugf("findAliasInCache,packagePath=%s,alias=%s,ok=%t\n", packagePath, aliasCache, ok)
				if ok {
					alias = aliasCache
				} else {
					lastIndex := strings.LastIndex(packagePath, "/")
					alias = packagePath[lastIndex+1:]
				}
			}

			log.Debugf("current_file=%s packagePath=%s, alias=%s\n", this.currentFile, packagePath, alias)

			this.currentFileImports = append(this.currentFileImports, &importMeta{
				Alias: alias,
				Path:  packagePath,
			})
		}
	}

	for _, decl := range file.Decls {

		genDecl, ok := decl.(*ast.GenDecl)

		if ok {
			for _, spec := range genDecl.Specs {

				typeSpec, ok := spec.(*ast.TypeSpec)

				if ok {
					if this.inIgnoreNode(typeSpec.Name.Name) {
						continue
					}

					interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
					if ok {
						this.visitInterfaceFunctions(typeSpec.Name.Name, interfaceType)
					}

					structType, ok := typeSpec.Type.(*ast.StructType)
					if ok {
						this.visitStructFields(typeSpec.Name.Name, structType)
					}

				}
			}
		}

	}

	for _, decl := range file.Decls {

		funcDecl, ok := decl.(*ast.FuncDecl)
		if ok {
			this.visitFunc(funcDecl)
		}

	}

}

func (this *analysisTool) visitStructType(name string, structType *ast.StructType, isTest bool) {

	strutMeta1 := &structMeta{
		baseInfo: baseInfo{
			FilePath:    this.currentFile,
			PackagePath: this.currentPackagePath,
		},
		Name:        name,
		MethodSigns: []string{},
		category:    StructCategory,
		isTest:      isTest,
	}

	this.structMetas = append(this.structMetas, strutMeta1)

}

func (this *analysisTool) visitStructFields(structName string, structType *ast.StructType) {

	sourceStruct1 := this.findStruct(this.currentPackagePath, structName)

	sourceStruct1.UML = this.structToUML(structName, structType, sourceStruct1)

	for _, field := range structType.Fields.List {
		this.visitStructField(sourceStruct1, field)
	}
}

func (this *analysisTool) visitStructField(sourceStruct1 *structMeta, field *ast.Field) {

	fieldNames := this.IdentsToString(field.Names)

	targetStruct1, isarray := this.analysisTypeForDependencyRelation(field.Type)

	if targetStruct1 != nil {

		if fieldNames == "" {

			d := DependencyRelation{
				source:      sourceStruct1,
				target:      targetStruct1,
				uml:         sourceStruct1.UniqueNameUML() + " -|> " + targetStruct1.UniqueNameUML(),
				inheritance: true,
			}

			this.dependencyRelations = append(this.dependencyRelations, &d)

		} else {

			if isarray {

				d := DependencyRelation{
					source: sourceStruct1,
					target: targetStruct1,
					uml:    sourceStruct1.UniqueNameUML() + " ---> \"*\" " + targetStruct1.UniqueNameUML() + " : " + fieldNames,
				}

				this.dependencyRelations = append(this.dependencyRelations, &d)

			} else {
				d := DependencyRelation{
					source: sourceStruct1,
					target: targetStruct1,
					uml:    sourceStruct1.UniqueNameUML() + " ---> " + targetStruct1.UniqueNameUML() + " : " + fieldNames,
				}

				this.dependencyRelations = append(this.dependencyRelations, &d)

			}

		}

	}

}

func (this *analysisTool) isGoBaseType(type1 string) bool {

	baseTypes := []string{"bool", "byte", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64", "complex64", "complex128", "string", "uintptr", "rune", "error"}

	if sliceContains(baseTypes, type1) {
		return true
	}

	return false
}

func (this *analysisTool) findStructByAliasAndStructName(alias string, structName string) *structMeta {

	if alias == "" && this.isGoBaseType(structName) {
		return nil
	}

	packagepath := this.findPackagePathByAlias(alias, structName)

	if packagepath != "" {
		return this.findStruct(packagepath, structName)
	}

	return nil
}

func (this *analysisTool) analysisTypeForDependencyRelation(t ast.Expr) (structMeta1 *structMeta, isArray bool) {

	structMeta1 = nil
	isArray = false

	ident, ok := t.(*ast.Ident)
	if ok {
		structMeta1 = this.findStructByAliasAndStructName("", ident.Name)
		isArray = false
		return
	}

	starExpr, ok := t.(*ast.StarExpr)
	if ok {
		structMeta1, isArray = this.analysisTypeForDependencyRelation(starExpr.X)
		return
	}

	arrayType, ok := t.(*ast.ArrayType)
	if ok {
		eleStructName, _ := this.analysisTypeForDependencyRelation(arrayType.Elt)
		structMeta1 = eleStructName
		isArray = true
		return
	}

	mapType, ok := t.(*ast.MapType)
	if ok {
		valueStructMeta1, _ := this.analysisTypeForDependencyRelation(mapType.Value)
		structMeta1 = valueStructMeta1
		isArray = true
		return
	}

	selectorExpr, ok := t.(*ast.SelectorExpr)
	if ok {
		alias := this.typeToString(selectorExpr.X, false)
		structMeta1 = this.findStructByAliasAndStructName(alias, this.typeToString(selectorExpr.Sel, false))
		isArray = false
		return
	}

	return
}

func (this *analysisTool) structToUML(name string, structType *ast.StructType, me *structMeta) string {
	classUML := "class " + name + me.ColorfulUML() + " " + this.structBodyToString(structType)
	return fmt.Sprintf("namespace %s {\n %s \n}", this.packagePathToUML(this.currentPackagePath), classUML)
}

func (this *analysisTool) packagePathToUML(packagePath string) string {
	return packagePathToUML(packagePath)
}

func (this *analysisTool) structBodyToString(structType *ast.StructType) string {

	result := "{\n"

	for _, field := range structType.Fields.List {
		result += "  " + this.fieldToString(field) + "\n"
	}

	result += "}"

	return result

}

func (this *analysisTool) visitInterfaceType(name string, interfaceType *ast.InterfaceType) {

	interfaceInfo1 := &structMeta{
		baseInfo: baseInfo{
			FilePath:    this.currentFile,
			PackagePath: this.currentPackagePath,
		},
		Name:     name,
		category: InterfaceCategory,
	}

	this.structMetas = append(this.structMetas, interfaceInfo1)
}

func (this *analysisTool) interfaceToUML(name string, interfaceType *ast.InterfaceType, me *structMeta) string {
	interfaceUML := "interface " + name + me.ColorfulUML() + " " + this.interfaceBodyToString(interfaceType)
	return fmt.Sprintf("namespace %s {\n %s \n}", this.packagePathToUML(this.currentPackagePath), interfaceUML)
}

func (this *analysisTool) funcParamsResultsToString(funcType *ast.FuncType) string {

	funcString := "("

	if funcType.Params != nil {
		for index, field := range funcType.Params.List {
			if index != 0 {
				funcString += ","
			}

			funcString += this.fieldToString(field)
		}
	}

	funcString += ")"

	if funcType.Results != nil {

		if len(funcType.Results.List) >= 2 {
			funcString += "("
		}

		for index, field := range funcType.Results.List {
			if index != 0 {
				funcString += ","
			}

			funcString += this.fieldToString(field)
		}

		if len(funcType.Results.List) >= 2 {
			funcString += ")"
		}
	}

	return funcString

}

func (this *analysisTool) findStruct(packagePath string, structName string) *structMeta {

	for _, structMeta1 := range this.structMetas {
		if structMeta1.Name == structName && structMeta1.PackagePath == packagePath {
			return structMeta1
		}
	}

	return nil
}

func (this *analysisTool) findTypeAlias(packagePath string, structName string) *typeAliasMeta {

	for _, typeAliasMeta1 := range this.typeAliasMetas {
		if typeAliasMeta1.Name == structName && typeAliasMeta1.PackagePath == packagePath {
			return typeAliasMeta1
		}
	}

	return nil
}

func (this *analysisTool) visitFunc(funcDecl *ast.FuncDecl) {

	this.debugFunc(funcDecl)

	packageAlias, structName := this.findStructTypeOfFunc(funcDecl)

	if structName != "" {

		packagePath := ""
		if packageAlias == "" {
			packagePath = this.currentPackagePath
		}

		structMeta := this.findStruct(packagePath, structName)
		if structMeta != nil {
			methodSign := this.createMethodSign(funcDecl.Name.Name, funcDecl.Type)
			structMeta.MethodSigns = append(structMeta.MethodSigns, methodSign)
		}
	}

}

func (this *analysisTool) visitInterfaceFunctions(name string, interfaceType *ast.InterfaceType) {

	if name == "CryptoSuite" {
		log.Println("haha")
	}
	methods := []string{}

	for _, field := range interfaceType.Methods.List {

		funcType, ok := field.Type.(*ast.FuncType)

		if ok {
			methods = append(methods, this.createMethodSign(field.Names[0].Name, funcType))
		}
	}

	im := this.findStruct(this.currentPackagePath, name)
	im.MethodSigns = methods
	im.UML = this.interfaceToUML(name, interfaceType, im)
}

func (this *analysisTool) findStructTypeOfFunc(funcDecl *ast.FuncDecl) (packageAlias string, structName string) {

	if funcDecl.Recv != nil {

		for _, field := range funcDecl.Recv.List {

			t := field.Type

			ident, ok := t.(*ast.Ident)
			if ok {
				packageAlias = ""
				structName = ident.Name
			}

			starExpr, ok := t.(*ast.StarExpr)
			if ok {
				ident, ok := starExpr.X.(*ast.Ident)
				if ok {
					packageAlias = ""
					structName = ident.Name
				}

			}
		}
	}

	return
}

func (this *analysisTool) debugFunc(funcDecl *ast.FuncDecl) {

	log.Debug("func name=", funcDecl.Name)

	if funcDecl.Recv != nil {
		for _, field := range funcDecl.Recv.List {
			log.Debug("func recv, name=", field.Names, " type=", field.Type)
		}
	}

	if funcDecl.Type.Params != nil {
		for _, field := range funcDecl.Type.Params.List {
			log.Debug("func param, name=", field.Names, " type=", field.Type)
		}
	}

	if funcDecl.Type.Results != nil {
		for _, field := range funcDecl.Type.Results.List {
			log.Debug("func result, type=", field.Type)
		}
	}

}

func (this *analysisTool) IdentsToString(names []*ast.Ident) string {
	r := ""
	for index, name := range names {
		if index != 0 {
			r += ","
		}
		r += name.Name
	}

	return r
}

// 创建方法签名
func (this *analysisTool) createMethodSign(methodName string, funcType *ast.FuncType) string {

	methodSign := methodName + "("

	if funcType.Params != nil {
		for index, field := range funcType.Params.List {
			if index != 0 {
				methodSign += ","
			}
			methodSign += this.fieldToStringInMethodSign(field)
		}
	}

	methodSign += ")"

	if funcType.Results != nil {

		if len(funcType.Results.List) >= 2 {
			methodSign += "("
		}

		for index, field := range funcType.Results.List {
			if index != 0 {
				methodSign += ","
			}
			methodSign += this.fieldToStringInMethodSign(field)
		}

		if len(funcType.Results.List) >= 2 {
			methodSign += ")"
		}
	}

	return methodSign
}

func (this *analysisTool) fieldToStringInMethodSign(f *ast.Field) string {

	argCount := len(f.Names)

	if argCount == 0 {
		argCount = 1
	}

	sign := ""

	for i := 0; i < argCount; i++ {
		if i != 0 {
			sign += ","
		}
		sign += this.typeToString(f.Type, true)
	}

	return sign
}

func (this *analysisTool) fieldToString(f *ast.Field) string {

	r := ""

	if len(f.Names) > 0 {

		for index, name := range f.Names {
			if index != 0 {
				r += ","
			}

			r += name.Name
		}

		r += " "

	}

	r += this.typeToString(f.Type, false)

	return r

}

func (this *analysisTool) typeToString(t ast.Expr, convertTypeToUnqiueType bool) string {

	ident, ok := t.(*ast.Ident)
	if ok {
		if convertTypeToUnqiueType {
			return this.addPackagePathWhenStruct(ident.Name)
		} else {
			return ident.Name
		}
	}

	starExpr, ok := t.(*ast.StarExpr)
	if ok {
		return "*" + this.typeToString(starExpr.X, convertTypeToUnqiueType)
	}

	arrayType, ok := t.(*ast.ArrayType)
	if ok {
		return "[]" + this.typeToString(arrayType.Elt, convertTypeToUnqiueType)
	}

	mapType, ok := t.(*ast.MapType)
	if ok {
		return "map[" + this.typeToString(mapType.Key, convertTypeToUnqiueType) + "]" + this.typeToString(mapType.Value, convertTypeToUnqiueType)
	}

	chanType, ok := t.(*ast.ChanType)
	if ok {
		return "chan " + this.typeToString(chanType.Value, convertTypeToUnqiueType)
	}

	funcType, ok := t.(*ast.FuncType)
	if ok {
		return "func" + this.funcParamsResultsToString(funcType)
	}

	interfaceType, ok := t.(*ast.InterfaceType)
	if ok {
		return "interface " + strings.Replace(this.interfaceBodyToString(interfaceType), "\n", " ", -1)
	}

	selectorExpr, ok := t.(*ast.SelectorExpr)
	if ok {
		if convertTypeToUnqiueType {
			return this.findPackagePathByAlias(this.selectorExprToString(selectorExpr.X), selectorExpr.Sel.Name) + "." + selectorExpr.Sel.Name
		} else {
			return this.typeToString(selectorExpr.X, true) + "." + selectorExpr.Sel.Name
		}
	}

	structType, ok := t.(*ast.StructType)
	if ok {
		return "struct " + strings.Replace(this.structBodyToString(structType), "\n", " ", -1)
	}

	ellipsis, ok := t.(*ast.Ellipsis)
	if ok {
		return "... " + this.typeToString(ellipsis.Elt, convertTypeToUnqiueType)
	}

	parenExpr, ok := t.(*ast.ParenExpr)
	if ok {
		return " (" + this.typeToString(parenExpr.X, convertTypeToUnqiueType) + ")"
	}

	log.Error("typeToString ", reflect.TypeOf(t), " file=", this.currentFile, " expr=", this.content(t))

	return ""
}

func (this *analysisTool) selectorExprToString(t ast.Expr) string {

	ident, ok := t.(*ast.Ident)
	if ok {
		return ident.Name
	}

	log.Error("selectorExprToString ", reflect.TypeOf(t), " file=", this.currentFile, " expr=", this.content(t))

	return ""
}

func (this *analysisTool) addPackagePathWhenStruct(fieldType string) string {

	searchPackages := []string{this.currentPackagePath}

	for _, import1 := range this.currentFileImports {
		if import1.Alias == "." {
			searchPackages = append(searchPackages, import1.Path)
		}
	}

	for _, meta := range this.structMetas {
		if sliceContains(searchPackages, meta.PackagePath) && meta.Name == fieldType {
			return meta.PackagePath + "." + fieldType
		}
	}

	// 处理type TypeString string 这个情况。TypeString不作为节点存在
	for _, meta := range this.typeAliasMetas {
		if sliceContains(searchPackages, meta.PackagePath) && meta.Name == fieldType {
			return meta.PackagePath + "." + fieldType
		}
	}

	return fieldType
}

func (this *analysisTool) findAliasByPackagePath(packagePath string) string {
	result := ""

	if this.config.VendorDir != "" {
		absPath := path.Join(this.config.VendorDir, packagePath)
		if PathExists(absPath) {
			result = findGoPackageNameInDirPath(absPath)
		}
	}

	if this.config.GopathDir != "" {
		absPath := path.Join(this.config.GopathDir, "src", packagePath)
		if PathExists(absPath) {
			result = findGoPackageNameInDirPath(absPath)
		}
	}

	log.Debugf("packagepath=%s, alias=%s\n", packagePath, result)

	return result
}

func (this *analysisTool) existStructOrInterfaceInPackage(typeName string, packageName string) bool {
	structMeta1 := this.findStruct(this.currentPackagePath, typeName)
	if structMeta1 != nil {
		return true
	}

	return false
}

func (this *analysisTool) existTypeAliasInPackage(typeName string, packageName string) bool {
	meta1 := this.findTypeAlias(this.currentPackagePath, typeName)
	if meta1 != nil {
		return true
	}

	return false
}

func (this *analysisTool) findPackagePathByAlias(alias string, structName string) string {

	if alias == "" {

		if this.existStructOrInterfaceInPackage(structName, this.currentPackagePath) {
			return this.currentPackagePath
		}

		if this.existTypeAliasInPackage(structName, this.currentPackagePath) {
			// 忽略别名类型
			return ""
		}

		matchedImportMetas := []*importMeta{}

		for _, importMeta := range this.currentFileImports {
			if importMeta.Alias == "." {
				matchedImportMetas = append(matchedImportMetas, importMeta)
			}
		}

		if len(matchedImportMetas) > 1 {

			for _, matchedImportMeta := range matchedImportMetas {

				if this.existStructOrInterfaceInPackage(structName, matchedImportMeta.Path) {
					log.Debugf("findPackagePathByAlias, alias=%s, packagePath=%s\n", alias, matchedImportMeta.Path)
					return matchedImportMeta.Path
				}

				if this.existTypeAliasInPackage(structName, matchedImportMeta.Path) {
					// 忽略别名类型
					return ""
				}

			}

		}

		currentFileImportsjson, _ := json.Marshal(this.currentFileImports)
		log.Warnf("找不到包的全路径，包名为%s，type name=%s, 在%s文件, matchedImportMetas=%d, currentFileImports=%s", alias, structName, this.currentFile, len(matchedImportMetas), currentFileImportsjson)

		return alias

	} else {

		for _, importMeta := range this.currentFileImports {
			if importMeta.Path == alias {
				log.Debugf("findPackagePathByAlias, alias=%s, packagePath=%s\n", alias, alias)
				return alias
			}
		}

		matchedImportMetas := []*importMeta{}

		for _, importMeta := range this.currentFileImports {
			if importMeta.Alias == alias {
				matchedImportMetas = append(matchedImportMetas, importMeta)
			}
		}

		if len(matchedImportMetas) == 1 {
			log.Debugf("findPackagePathByAlias, alias=%s, packagePath=%s\n", alias, matchedImportMetas[0].Path)
			return matchedImportMetas[0].Path
		}

		if len(matchedImportMetas) > 1 {

			for _, matchedImportMeta := range matchedImportMetas {

				if this.existStructOrInterfaceInPackage(structName, matchedImportMeta.Path) {
					log.Debugf("findPackagePathByAlias, alias=%s, packagePath=%s\n", alias, matchedImportMeta.Path)
					return matchedImportMeta.Path
				}

				if this.existTypeAliasInPackage(structName, matchedImportMeta.Path) {
					// 忽略别名类型
					return ""
				}

			}

		}

		currentFileImportsjson, _ := json.Marshal(this.currentFileImports)
		log.Warnf("找不到包的全路径，包名为%s，type name=%s, 在%s文件, matchedImportMetas=%d, currentFileImports=%s", alias, structName, this.currentFile, len(matchedImportMetas), currentFileImportsjson)

		return alias

	}

}

func (this *analysisTool) interfaceBodyToString(interfaceType *ast.InterfaceType) string {

	result := " {\n"

	for _, field := range interfaceType.Methods.List {

		funcType, ok := field.Type.(*ast.FuncType)

		if ok {
			result += "  " + this.IdentsToString(field.Names) + this.funcParamsResultsToString(funcType) + "\n"
		}

	}

	result += "}"

	return result

}

func (this *analysisTool) content(t ast.Expr) string {
	bytes, err := ioutil.ReadFile(this.currentFile)
	if err != nil {
		log.Error("读取文件", this.currentFile, "失败", err)
		return ""
	}

	return string(bytes[t.Pos()-1 : t.End()-1])
}

/**
 * 查找interface有哪些实现的Struct
 */
func (this *analysisTool) findInterfaceImpls(interfaceMeta1 *structMeta) []*structMeta {
	metas := []*structMeta{}

	for _, structMeta1 := range this.structMetas {
		if structMeta1.category != StructCategory {
			continue
		}
		if this.inheritance(interfaceMeta1, structMeta1) {
			metas = append(metas, structMeta1)
		}
	}

	return metas
}

func (this *analysisTool) UML() string {

	uml := ""

	for _, structMeta1 := range this.structMetas {
		uml += structMeta1.UML
		uml += "\n"
	}

	for _, d := range this.dependencyRelations {
		uml += d.uml
		uml += "\n"
	}

	for _, interfaceMeta1 := range this.structMetas {
		if interfaceMeta1.category != InterfaceCategory {
			continue
		}

		structMetas := this.findInterfaceImpls(interfaceMeta1)
		for _, structMeta := range structMetas {
			uml += structMeta.implInterfaceUML(interfaceMeta1)
		}
	}

	return "@startuml\n" + uml + "@enduml"
}

func (this *analysisTool) OutputToFile(logdir string, nodename string, nodedepth uint16, showtest bool) {
	var uml string
	var logfile string

	if nodedepth < 1 {
		nodedepth = 1
	}

	if nodename == "" {
		uml = this.UML()
		logfile = logdir + "/all.puml"
	} else {
		uml = this.filterUML(nodename, nodedepth, showtest)
		logfile += fmt.Sprintf("%s/node-%s-%d-%v.puml", logdir, nodename, nodedepth, showtest)
	}
	ioutil.WriteFile(logfile, []byte(uml), 0666)
	log.Infof("数据已保存到%s\n", logfile)

}

func (tool *analysisTool) getMyParents(meta *structMeta) []*structMeta {
	parents := make([]*structMeta, 0, 5)
	for _, v := range tool.dependencyRelations {
		if v.inheritance && v.source == meta {
			parents = append(parents, v.target)
		}
	}
	return parents
}
func (tool *analysisTool) inIgnoreNode(s string) bool {
	for _, v := range tool.config.IgnoreNodes {
		if v == s {
			return true
		}
	}
	return false
}
