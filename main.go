package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
)

var (
	file       = flag.String("file", "", "path to file")
	structName = flag.String("struct", "", "name of struct")
	tableName  = flag.String("table", "", "name of table")
	sqlFile    = flag.String("sql", "", "path to generated sql file")
	genSuffix  = flag.String("suffix", "_generated", "suffix to generated file")
)

type Field struct {
	Name          string
	Type          string
	Default       string
	NotNull       bool
	Uniqeu        bool
	AutoIncrement bool
	Primary       bool
	Index         bool
	Foreign       bool
	ForeignTable  string
}

func (f *Field) parseTag(tag string) {
	tag = tag[1:len(tag)] //избавляемся от `
	rxp := regexp.MustCompilePOSIX(`db:\"([a-z0-9\-_]+)\"`)
	name := rxp.FindString(tag)
	if name == "-" {
		return
	}
	f.Name = name[4 : len(name)-1]
	rxp = regexp.MustCompilePOSIX(`id_([a-z0-9]+)`)
	if rxp.MatchString(f.Name) {
		f.Foreign = true
		f.ForeignTable = f.Name[3:]
	}
	rxp = regexp.MustCompilePOSIX(`gen:"([a-z0-9\-_,\'\'\(\)]+)"`)
	gen := rxp.FindString(tag)
	if len(gen) < 6 {
		return
	}
	opts := strings.Split(gen[5:len(gen)-1], ",")
	if len(opts) > 0 {
		f.Type = opts[0]
	}
	for _, opt := range opts {
		if opt == f.Type {
			continue
		}
		switch opt {
		case "autoincrement":
			f.AutoIncrement = true
		case "notnull":
			f.NotNull = true
		case "primary":
			f.Primary = true
		case "unique":
			f.Uniqeu = true
		case "index":
			f.Index = true
		default:
			rxp = regexp.MustCompilePOSIX(`default\(.+\)`)
			def := rxp.FindString(opt)
			f.Default = def[8 : len(def)-1]
		}
	}
}

func (f *Field) genereateSQL() (sql string) {
	sql = f.Name + " " + f.Type
	if f.AutoIncrement {
		sql += " AUTO_INCREMENT"
	}
	if f.NotNull {
		sql += " NOT NULL"
	}
	if f.Default != "" {
		sql += " DEFAULT " + f.Default
	}
	if f.Uniqeu {
		sql += " UNIQUE"
	}
	if f.Primary {
		sql += ",\n"
		sql += "PRIMARY KEY(" + f.Name + ")"
	}
	if f.Foreign {
		sql += ",\n"
		sql += "CONSTRAINT FOREIGN KEY (" + f.Name + ") REFERENCES " + f.ForeignTable + "(id)"
	}
	if f.Index {
		sql += ",\n"
		sql += "INDEX(" + f.Name + "_col)"
	}
	return sql
}

func NewField(tagStr string) (f Field) {
	f.parseTag(tagStr)
	return f
}

type Table struct {
	Name   string
	Fields []Field
}

func (t Table) genereateSQL() (sql string) {
	sql = "CREATE TABLE " + t.Name + " {\n"
	var cols []string
	for _, f := range t.Fields {
		cols = append(cols, "\t"+f.genereateSQL())
	}
	sql += strings.Join(cols, ",\n")
	sql += "\n}"
	return sql
}

func (t Table) genereateSelectConst() (cnst string) {
	cnst = "select" + *structName + " = `SELECT "
	var cols []string
	for _, f := range t.Fields {
		cols = append(cols, f.Name)
	}
	cnst += strings.Join(cols, ", ")
	cnst += " FROM " + t.Name + " /*condition*/`"
	return cnst
}

func (t Table) genereateInsertConst() (cnst string) {
	cnst = "insert" + *structName + " = `INSERT INTO " + t.Name
	cnst += " ("
	var cols []string
	for _, f := range t.Fields {
		cols = append(cols, f.Name)
	}
	cnst += strings.Join(cols, ", ")
	cnst += " )"
	cnst += " VALUES ("
	cols = cols[0:0]
	for _, f := range t.Fields {
		cols = append(cols, ":"+f.Name)
	}
	cnst += strings.Join(cols, ", ")
	cnst += " )`"
	return cnst
}

func (t Table) genereateCountConst() (cnst string) {
	cnst = "count" + *structName + " = `SELECT COUNT(*) FROM " + t.Name + "`"
	return cnst
}

func (t Table) genereateUpdateConst() (cnst string) {
	cnst = "update" + *structName + " = `UPDATE " + t.Name + " SET "
	var cols []string
	var primary string
	for _, f := range t.Fields {
		if f.Primary {
			primary = f.Name + "=:" + f.Name
			continue
		}
		cols = append(cols, f.Name+"=:"+f.Name)
	}
	cnst += strings.Join(cols, ", ")
	cnst += " WHERE " + primary + "`"
	return cnst
}

func (t Table) genereateLimitFunc() (textFunc string) {
	return `
func ` + *structName + `SelectLimit(limit int) (models []` + *structName + `, err error) {
  query := easydb.Condition(insert` + *structName + `, " limit ? ")
  err = easydb.Select(&models, query, limit)
  if err != nil {
    return models, err
  }
  return models, nil
}
`
}

func (t Table) genereateCountFunc() (textFunc string) {
	return `
func ` + *structName + `Count() (count int, err error) {
  err = easydb.Get(&count, count` + *structName + `)
  if err != nil {
    return count, err
  }
  return count, err
}
`
}

func (t Table) genereateUpdateFunc() (textFunc string) {
	return `
func (model *` + *structName + `) Update() (err error) {
  _, err = easydb.NamedExec(update` + *structName + `, model)
  if err != nil {
    return err
  }
  return nil
}
`
}

func (t Table) genereateInsertFunc() (textFunc string) {
	return `
func (model *` + *structName + `) Insert() (err error) {
  var res sql.Result
  res, err = easydb.NamedExec(insert` + *structName + `, model)
  if err != nil {
    return err
  }
  model.ID, err = res.LastInsertId()
  if err != nil {
    return err
  }
  return nil
}
`
}

func main() {
	flag.Parse()
	if *file == "" {
		fmt.Println("Укажите файл для парсинга")
		return
	}
	if *structName == "" {
		fmt.Println("Укажите имя структуры")
		return
	}
	if *tableName == "" {
		fmt.Println("Укажите имя таблицы")
		return
	}
	_, err := os.Stat(*file)
	if err != nil {
		fmt.Println(*file, "не существует")
		return
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, *file, nil, parser.ParseComments)
	if err != nil {
		fmt.Println("Ошибка парсинга исходника:", err)
		return
	}
	for _, decl := range f.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok {
			for _, spec := range genDecl.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					if typeSpec.Name.Name == *structName {
						table := new(Table)
						table.Name = *tableName
						if structType, ok := typeSpec.Type.(*ast.StructType); ok {
							for _, field := range structType.Fields.List {
								if field.Tag != nil {
									f := NewField(field.Tag.Value)
									if f.Name != "-" {
										table.Fields = append(table.Fields, f)
									}
								}
							}
						}
						if *sqlFile != "" {
							ioutil.WriteFile(*sqlFile, []byte(table.genereateSQL()), 0644)
						}
						src := "package " + f.Name.Name + "\n\n"
						src += "import (\n"
						src += "\t\"database/sql\"\n"
						src += "\t\"github.com/suntoucha/easydb\"\n)\n\n"
						src += "const (\n"
						src += "\t" + table.genereateCountConst() + "\n"
						src += "\t" + table.genereateSelectConst() + "\n"
						src += "\t" + table.genereateUpdateConst() + "\n"
						src += "\t" + table.genereateInsertConst() + "\n)\n\n"
						src += table.genereateLimitFunc() + "\n"
						src += table.genereateCountFunc() + "\n"
						src += table.genereateInsertFunc() + "\n"
						src += table.genereateUpdateFunc() + "\n"
						ioutil.WriteFile((*file)[:len(*file)-3]+*genSuffix+".go", []byte(src), 0644)

					}
				}
			}
		}
	}
}
