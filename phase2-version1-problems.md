
当前版本还存在很多细节问题：

- service template 的创建页面太宽了，应该使用一列来完成而不是两列
- Compose definition 中的东西太少了，所有的可配置都应该写出来，该是下拉框的使用下拉框，该是可添加的多行就添加为多行。其保存的数据就应该是一个yaml格式的而不是每个字段都还有自己的sql字段，yaml也是交给前端自己去解析，后端只是在保存时多确认一遍是否yaml是否格式正确、是否符合compose语法。
- service template 所有可以手动输入的地方都应该允许使用变量，包括比如volume的路径中、template类型的文件，在生成最终的yaml文件的时候需要替换。当前template关联的文件的内容、路径属于内置变量，当前和所有的（所有的直接表示为列表）server的ip、用户、名称也属于内置变量
- service template 的 preview server应该是放在预览上面，validation不需要展示出来，只需要在有问题的配置下显示错误
- services 和 runtime Resources中的 services实际上是同一个东西，runtime Resources中就不需要了，但是services中对于属于service template创建的容器需要标记是什么template，而不属于的就直接显示不受管理
- server 需要有地方能编辑每个server的自定义变量
- 所有的页面都应该充满屏幕，如果超出屏幕，则需要这一块card自己内部支持滚动而不是让整个屏幕滚动
- overview在没有服务器的时候应该显示别的东西，比如引导用户之类的
- 将标题显示在panel-header，原来显示标题的地方显示当前正在执行的任务，一次显示一个，多个任务自动向上滚动循环显示
- services 放在第一个，service template放在最后
