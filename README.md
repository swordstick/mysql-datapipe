## MYSQL-DATAPIPE

MYSQL数据变形同步工具。<br>
支持MYSQL之间完成初始化，同步推送。<br>
从源库中抽取快照数据和变更操作，表名变更和字段过滤之后，同步到其他的MYSQL。<br>


## 安装


### 编译源码

```
$ export $GOBIN={YOUR_GOBIN}
$ export $GOPATH={YOUR_GOPAH}
$ cp {源码} {GOPATH/src}
$ cd {YOUR_GOPATH}
$ GO INSTALL src/cmd/datapipe.go
```

## 功能

* 初始化数据至目标库
* 持续获取变更推送到目标库
* 变更表名
* 筛选需要同步的字段
* 支持各种DDL操作同步

## 用法

### 启动方法

```
// -c不指定时，默认在本文件夹寻找 datapipe.toml
./datapipe -c {CONFIGFDIR}/datapipe.toml 
```


### 配置文件修改

```
# CONFIG FILE FOR DTAPIPE

addr = "127.0.0.1:3306"	#源数据库地址
user = "root"			   #源数据库账号
password = "davidwan"	#源数据库密码
server_id = 665		#该进程伪装的从库SERVER_ID,注意不要和源库及其从库相同
flavor = "mysql"		#mysql或者mariadb
taddr = "127.0.0.1:3307"	#目标数据库地址
tuser = "root"			#目标数据库账号
tpassword = "davidwan"	#目标数据库密码
dumpthreads = 20			#dump推送时并发数，暂时被代码内指定为20，此处为保留参数
data_dir = "./master"	#master.info文件保存位置，记录当前同步的postion
log_level = "debug"
log_dir = "./log"
logfile = "databus_error.log"
[dump]		
mysqldump = "/usr/local/mysql/bin/mysqldump"  #本机mysqldump地址
tables = []			#希望同步的表名，不填表示table_db所有的表都默认同步
table_db = "test"	#指定一个同步的库名
Ignore_tables = ["wp_posts1","wp_posts"]  #指定希望过滤的表名
discard_err = false			#是否错误信息

[Filters]
[[Filter]]
Table = "wp_postmeta_opt"		#过滤的表名
Columns = ["meta_id","post_id","meta_key"] #该表下希望被同步的字段

[[Filter]]
Table = "wp_posts1"
Columns = ["comment_status","post_password"]


[Optimus]

[[Optimu]]
Source = "wp_postmeta_opt"			#源库中表名
Target = "wp_postmeta"		#目标库中表名，转换表名

[[Optimu]]
Source = "wp_posts5_opt"
Target = "wp_posts5"
```

### 注意事项

#### 表名转换和字段过滤表的处理

* 被指定为表名和字段过滤的表,DDL操作将被屏蔽,因此导致的后面操作无法写入，不用担心，只要往目标表手动加入该变更即可自动恢复
* 若无这两项配置，保留[Filters]和[Optimus],其下的[[Filter]]和[[Optimu]]删除即可

#### 偏移位置

* 偏移位置保存时机，DUMP快照完成写入后,定时器，KILL PROCESS_PID 进程退出时，每个ALTER操作和DML操作完成后。
* 偏移位置保存在master文件中，master.info文件中
* 启动时检查master.info如果，已经有了有效内容，将SKIP DUMP的初始化过程
* 启动时检查master.info如果，源库的地址更换了，将使用配置文件的地址替换，但FILE和POS位置沿用，请注意
* master.info中保存，源库的连接地址和偏移位置

#### 同步选择的范围

 
* 可以选择所有库，多个库，或者1个库，以及一个库中的若干表
* 按照参数生效级别，Ignore_tables > tables(即指定同步或[]默认不指定的表参数) 


#### 同步的操作

* 同步的操作包含DML,DDL
* 第一次初始化，不会生产建表语句，所以初始化时请自行建立表，运行之后，表的建立等操作会自动同步

#### 工具的关闭和启动

* 直接KILL即可，下次启动会按照master.info位置重新同步

#### 自行初始化

* 放弃工具自动完成初始化表工作，只需要在master.info中填入获取的偏移信息即可

#### telnet 

* telnet登陆 monitorip && monitorclient，即可使用help查看到各类命令，包含在线修改loglevel
* auth 认证 进行有效操作前，需要auth认证，密码为auth配置的字符串

## 性能

* 该工具目前主要核心在表名转换和字段变形，性能方面暂不是最核心部分
* DUMP的推送过程使用了并发池，但测试显示，提高比例并不大，测试中10G/Hour的DUMP推送速度
* 若希望提升表的变更同步，可以启动多个datapipe分别同步变更压力大的表

## 版本

* v1.0

## 版本变更计划

* 加入HTTP监控接口，提供偏移时间值，当前配置读取，满足通用监控需求
* 更丰富的数据总线请参考mysql-databus项目
* 本项目定位为MYSQL间同步工具


## 作者

swordstick<br>
www.dbathread.com
