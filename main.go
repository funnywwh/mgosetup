package main

import (
	"archive/zip"
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/mgo.v2"
	"gopkg.in/yaml.v2"
)

var rootname string
var rootpwd string
var dbroot string
var logpath string
var serviceName string
var unzippath string

var mongoexe string

func main() {
	flag.StringVar(&rootname, "ru", "root", "name of root")
	flag.StringVar(&rootpwd, "rp", "root", "password of root")
	flag.StringVar(&dbroot, "dbpath", "d:/data", "database path")
	flag.StringVar(&logpath, "logpath", "d:/data/log", "log path")
	flag.StringVar(&serviceName, "service", "mongodb", "name of service name")
	flag.StringVar(&unzippath, "unzip", "d:/data/bin", "path unzip mongo zip file to")
	flag.Parse()
	defer func() {
		recover()
		var line string
		log.Println("press <ENTER> to exit")
		fmt.Scanln(&line)
	}()
	var mgozipfile string
	if len(flag.Args()) < 1 {
		mgozipfile = "./mongodb.zip"
		_, err := os.Stat(mgozipfile)
		if err != nil {
			log.Println(err)
			flag.Usage()

			return
		}
	} else {
		mgozipfile = flag.Arg(0)
	}
	var err error
	var mgocmd *exec.Cmd
	var out string
	//stoping service
	log.Println("stoping service")
	Run("net", "stop", serviceName)
	Run("net", "stop", "MongoDB")

	//remove service
	log.Println("remove service")
	mgocmd, err = RunMgo(
		"--remove",
		"--serviceName", serviceName,
	)

	//delete install path
	err = os.RemoveAll(unzippath)
	if err != nil {
		log.Println(err)
		return
	}

	//create unzip to path
	os.Mkdir(unzippath, 0666)
	//create db path
	dbpath := fmt.Sprintf("%s/db", dbroot)
	os.Mkdir(dbpath, 0666)
	//create log path
	os.Mkdir(logpath, 0666)

	//unzip install zip file
	err = Unzip(mgozipfile)
	if err != nil {
		log.Println(err)
		return
	}
	//run no auth
	log.Println("run no auth to create root user")
	mgocmd, err = RunMgo("-dbpath", dbpath, "--noauth")
	if err != nil {
		log.Println(err)
		return
	}
	//create root user
	log.Println("create root user")
	err = AddRoot()
	if err != nil {
		log.Println(err)
		return
	}
	mgocmd.Process.Kill()
	//create mongo.yaml
	log.Println("create mongo.yaml")
	var configname = fmt.Sprintf("%s/mongo.yaml", dbroot)
	err = CreateConfigFile(configname, dbpath)
	if err != nil {
		log.Println(err)
		return
	}
	//install as service
	log.Println("install as service")
	mgocmd, err = RunMgo(
		"--dbpath", dbpath,
		"--logpath", fmt.Sprintf("%s/log.txt", logpath),
		"-f", configname,
		"--install",
		"--serviceName", serviceName,
	)
	if err != nil {
		log.Println(err)
		return
	}
	mgocmd.Wait()
	if err != nil {
		log.Println(err)
		return
	}
	//starting service
	log.Println("starting service")
	out, err = Run("net", "start", serviceName)
	if err != nil {
		log.Println(out)
		log.Println(err)
		return
	}
	log.Println(out)
}

func CreateConfigFile(name, dbpath string) (err error) {

	yamlbs, err := yaml.Marshal(map[string]interface{}{
		"security": map[string]interface{}{
			"authorization": "enabled",
		},
		"storage": map[string]interface{}{
			"dbPath": dbpath,
		},
	})
	if err != nil {
		return
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(yamlbs)
	return
}
func Unzip(zipfile string) (err error) {
	zf, err := zip.OpenReader(zipfile)
	if err != nil {
		log.Println(err)
		return
	}
	defer zf.Close()
	for _, f := range zf.File {
		dir, fname := filepath.Split(f.Name)
		outname := fmt.Sprintf("%s/%s", unzippath, f.Name)
		log.Println(outname)
		if fname == "mongod.exe" {
			mongoexe = outname
		}
		_ = mongoexe
		if len(dir) > 0 {
			os.Mkdir(fmt.Sprintf("%s/%s", unzippath, dir), 0666)
		}
		func() {
			izf, err := f.Open()
			if err != nil {
				return
			}
			defer izf.Close()
			of, err := os.OpenFile(outname, os.O_CREATE|os.O_WRONLY, f.FileInfo().Mode())
			if err != nil {
				return
			}
			defer of.Close()
			_, err = io.Copy(of, izf)
		}()
		if err != nil {
			if err != io.EOF {
				log.Println(err)
				return
			}
		}
	}
	return
}
func AddRoot() (err error) {
	s, err := mgo.Dial("localhost")
	if err != nil {
		log.Println(err)
		return
	}
	db := s.DB("admin")
	var user mgo.User
	user.Username = rootname
	user.Password = rootpwd
	user.Roles = append(user.Roles, mgo.RoleRoot)
	err = db.UpsertUser(&user)
	if err != nil {
		log.Println(err)
		return
	}
	return
}
func Run(cmd string, args ...string) (out string, err error) {
	c := exec.Command(cmd, args...)
	outbs, err := c.CombinedOutput()
	if err != nil {
		log.Println(err)
		return
	}
	out = string(outbs)
	return
}
func RunMgo(args ...string) (c *exec.Cmd, err error) {
	c = exec.Command(mongoexe, args...)
	stdout, err := c.StdoutPipe()
	if err != nil {
		log.Println(err)
		return
	}
	defer stdout.Close()
	go func() {
		err = c.Run()
	}()
	if err != nil {
		log.Println(err)
		return
	}
	var line string
	for {
		r := bufio.NewReader(stdout)
		line, err = r.ReadString('\n')

		if err != nil {
			if err != io.EOF {
				log.Println(err)
			}
			err = nil
			return
		}
		if strings.Contains(line, "waiting for connections on port") {
			log.Println("mongo running")
			break
		}
		fmt.Println(line)
	}
	return
}
