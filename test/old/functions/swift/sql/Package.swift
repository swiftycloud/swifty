// swift-tools-version:3.1

import PackageDescription

let package = Package(
    name: "run",
    dependencies:[
	.Package(url:"https://github.com/IBM-Swift/SwiftKueryMySQL",majorVersion:0),
    ]
)
