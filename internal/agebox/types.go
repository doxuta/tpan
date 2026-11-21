// Package agebox bọc thư viện filippo.io/age cho gói panel của M18 (mục 2.4 và
// 3.8 của docs/thay-doi/M18-thiet-ke.md): khoá sao lưu dạng chữ, mã hoá và giải
// mã theo dòng bằng chế độ khoá đối xứng (người nhận scrypt).
//
// Vì sao khoá đối xứng, không cặp khoá X25519: mã hoá khoá công khai chỉ giữ
// kín, không chứng thực người gửi; ai biết khoá công khai và ghi được vào
// bucket đều tạo được một gói "hợp lệ". Với khoá đối xứng, chỉ người có khoá
// mới tạo được bản mã giải ra được. File ra là khuôn age công khai: mở được
// bằng lệnh `age -d` của Ubuntu (đã thử, bản thử 12), không cần TPAN.
//
// File này là hợp đồng (đợt 0). Thợ đợt 1 viết agebox.go với đúng các hàm:
//
//	// GenerateKey sinh khoá mới: KeyPrefix + 52 ký tự base32 chữ thường (RFC
//	// 4648, không đệm) của 32 byte từ crypto/rand.
//	func GenerateKey() (system.Secret, error)
//	// CheckKey kiểm dạng khoá: ^tpan-key-1-[a-z2-7]{52}$. Lỗi không chép lại khoá.
//	func CheckKey(key system.Secret) error
//	// Fingerprint là 8 ký tự hex đầu của sha256 chuỗi khoá (để hiển thị).
//	func Fingerprint(key system.Secret) string
//	// Encrypt mở một luồng mã hoá vào dst: age.NewScryptRecipient(khoá),
//	// SetWorkFactor(WorkFactor). Close là bắt buộc (ghi khối cuối).
//	func Encrypt(dst io.Writer, key system.Secret) (io.WriteCloser, error)
//	// Decrypt mở luồng giải mã từ src: age.NewScryptIdentity(khoá),
//	// SetMaxWorkFactor(WorkFactor), để file có hệ số công cao hơn bị từ chối
//	// trước khi tốn RAM. Bản rõ đọc ra CHỈ được tin sau khi đọc tới io.EOF
//	// không lỗi (luồng nhả dữ liệu trước khi gặp khối cuối; file bị cắt cụt
//	// cho phần đầu hợp lệ rồi mới lỗi): người gọi giải mã ra file tạm rồi mới
//	// dùng file đó.
//	func Decrypt(src io.Reader, key system.Secret) (io.Reader, error)
//
// Không hàm nào in, ghi log hay đưa khoá vào lỗi.
package agebox

const (
	// KeyPrefix đứng đầu mọi khoá sao lưu (số 1 là khuôn khoá, để đổi về sau).
	KeyPrefix = "tpan-key-1-"
	// WorkFactor là log2 của tham số N của scrypt khi mã hoá, và là trần khi
	// giải mã. 15: khoảng 32 MB RAM, 0,1 giây (bản thử 12). Khoá đã là 256 bit
	// ngẫu nhiên nên hệ số không để chống dò, chỉ để đúng khuôn age.
	WorkFactor = 15
)
