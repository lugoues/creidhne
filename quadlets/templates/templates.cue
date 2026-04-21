@extern(embed)

package templates

Container: _ @embed(file=container.tpl,type=text)
Pod:       _ @embed(file=pod.tpl,type=text)
Volume:    _ @embed(file=volume.tpl,type=text)
Network:   _ @embed(file=network.tpl,type=text)
Kube:      _ @embed(file=kube.tpl,type=text)
Build:     _ @embed(file=build.tpl,type=text)
Image:     _ @embed(file=image.tpl,type=text)
Artifact:  _ @embed(file=artifact.tpl,type=text)
