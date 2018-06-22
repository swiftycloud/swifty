import Foundation
import Glibc

public typealias Byte = UInt8

var qfd: Int32
qfd = 3

func load(data: [Byte]) -> [String:String] {
	return try! JSONDecoder().decode([String:String].self, from: Data(bytes: data))
}

func save(obj: Encodable) -> Data {
	struct EncWrap: Encodable {
		let o: Encodable

		func encode(to encoder: Encoder) throws {
			try self.o.encode(to: encoder)
		}
	}

	let jstr = String(data: try! JSONEncoder().encode(EncWrap(o:obj)), encoding: .utf8)!
	return ("0:" + jstr).data(using: String.Encoding.utf8)!
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
