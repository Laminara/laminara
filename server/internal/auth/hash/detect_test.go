package hash

import "testing"

func TestDetect(t *testing.T) {
	cases := []struct {
		name      string
		stored    string
		scheme    string
		cost      int
		supported bool
		found     bool
	}{
		{name: "bcrypt 2a", stored: "$2a$12$Xk8Qq5t0nq9Vw1Zk3sJm3eO7Yb0nA7c1Q9wS2rT4uV6xY8zA0bC2d", scheme: "bcrypt", cost: 12, supported: true, found: true},
		{name: "bcrypt 2y низкая стоимость", stored: "$2y$04$abcdefghijklmnopqrstuv", scheme: "bcrypt", cost: 4, supported: true, found: true},
		{name: "argon2id", stored: "$argon2id$v=19$m=65536,t=1,p=2$c29tZXNhbHQ$hash", scheme: "argon2id", supported: true, found: true},
		{name: "argon2i не поддержан", stored: "$argon2i$v=19$m=4096,t=3,p=1$c29tZXNhbHQ$hash", scheme: "argon2i", supported: false, found: true},
		{name: "md5", stored: "5f4dcc3b5aa765d61d8327deb882cf99", scheme: "md5", supported: true, found: true},
		{name: "sha256", stored: "5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8", scheme: "sha256", supported: true, found: true},
		{name: "открытый текст", stored: "hunter2", found: false},
		{name: "пусто", stored: "  ", found: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, ok := Detect(test.stored)
			if ok != test.found {
				t.Fatalf("определение: получили %v, ждали %v", ok, test.found)
			}
			if !ok {
				return
			}
			if got.Scheme != test.scheme {
				t.Errorf("схема: получили %q, ждали %q", got.Scheme, test.scheme)
			}
			if got.Cost != test.cost {
				t.Errorf("стоимость: получили %d, ждали %d", got.Cost, test.cost)
			}
			if got.Supported != test.supported {
				t.Errorf("поддержка: получили %v, ждали %v", got.Supported, test.supported)
			}
		})
	}
}

func TestDetectRecognisesWhatWeProduce(t *testing.T) {
	for _, scheme := range []string{"argon2id", "bcrypt", "md5", "sha256", "sha512"} {
		stored, err := Produce(scheme, "пароль")
		if err != nil {
			t.Fatalf("%s: %v", scheme, err)
		}
		got, ok := Detect(stored)
		if !ok {
			t.Fatalf("%s: собственный хеш не распознан", scheme)
		}
		if got.Scheme != scheme {
			t.Fatalf("%s: распознан как %s", scheme, got.Scheme)
		}
	}
}

func TestBcryptCost(t *testing.T) {
	stored, err := ProduceCost("bcrypt", "пароль", 6)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Detect(stored)
	if !ok || got.Cost != 6 {
		t.Fatalf("стоимость не применилась: %+v", got)
	}
	verifier, err := Get("bcrypt")
	if err != nil {
		t.Fatal(err)
	}
	valid, err := verifier.Verify("пароль", stored)
	if err != nil || !valid {
		t.Fatalf("хеш со своей стоимостью не проверяется: %v %v", valid, err)
	}
	if _, err := ProduceCost("bcrypt", "пароль", 99); err == nil {
		t.Fatal("недопустимая стоимость должна отвергаться")
	}
	if _, err := ProduceCost("argon2id", "пароль", 6); err == nil {
		t.Fatal("argon2id не принимает стоимость, ошибка обязательна")
	}
	if AcceptsCost("argon2id") {
		t.Fatal("argon2id не должен объявлять поддержку стоимости")
	}
	if !AcceptsCost("bcrypt") {
		t.Fatal("bcrypt должен принимать стоимость")
	}
}
