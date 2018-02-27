import Foundation
import Glibc

public typealias Byte = UInt8

var qfd: Int32
qfd = Int32(CommandLine.arguments[1])!

var fd: Int32

fd = Int32(CommandLine.arguments[2])!
dup2(fd, 1)
close(fd)

fd = Int32(CommandLine.arguments[3])!
dup2(fd, 2)
close(fd)

func load(data: [Byte]) -> [String:String] {
	return try! JSONDecoder().decode([String:String].self, from: Data(bytes: data))
}

struct swyResult : Encodable {
	var Code: Int
	var Return: String

	enum CodingKeys: String, CodingKey {
		case Code = "code"
		case Return = "return"
	}
}

func save(obj: Encodable) -> Data {
	struct EncWrap: Encodable {
		let o: Encodable

		func encode(to encoder: Encoder) throws {
			try self.o.encode(to: encoder)
		}
	}

	let ret = swyResult(
		Code: 0,
		Return: String(data: try! JSONEncoder().encode(EncWrap(o:obj)), encoding: .utf8)!
	)

	return try! JSONEncoder().encode(ret)
}

while true {
	var msg = [Byte](repeating: 0x0, count: 1024)
	recv(qfd, &msg, 1024, 0)

	let args = load(data: msg)
	let ret = Main(args: args)
	var out = save(obj: ret)

	var pointer: UnsafePointer<UInt8>! = nil
	out.withUnsafeBytes { (u8Ptr: UnsafePointer<UInt8>) in
		pointer = u8Ptr
	}
	send(qfd, pointer, out.count, 0)
}
