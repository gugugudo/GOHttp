package main

func get_baidu() {
	ctx := HTTPCreate()
	// ====================== 第一步：GET请求初始注册页面 ======================
	HTTPOpen(ctx, "GET", "http://www.baidu.com")
	// 批量设置请求头
	HTTPSetRequestHeader(ctx, "Referer", "https://bfweb.hk.beanfun.com/game_zone/")
	HTTPSend(ctx, "")
	var res = ""
	res, _ = HTTPGetResponseBody(ctx)
	println(res)

	HTTPRemove(ctx)
}

// main 程序入口
func main() {
	get_baidu()

}
