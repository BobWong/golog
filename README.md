# golog

[Fork](https://github.com/davyxu/golog)


# 安装方法

	go get github.com/BobWong/golog

# 使用方法

* 基本使用

	var log *golog.Logger = golog.New("test")

	log.Debugln("hello world")

* 层级设置

	golog.SetLevelByString( "test", "info")


# 备注

感觉不错请star, 谢谢!

开源讨论群: 527430600

知乎: [http://www.zhihu.com/people/sunicdavy](http://www.zhihu.com/people/sunicdavy)
