using MailKit.Net.Smtp;
using Xunit;

namespace MailHogGM.Tests;

/// <summary>
/// Testes de integração ponta a ponta contra o MailHog-GM:
/// envia via SMTP (STARTTLS + AUTH, como no Gmail) e lê de volta pela API HTTP.
/// Cobre os e-mails de exemplo pedidos: confirmação de e-mail e token de reset.
/// </summary>
public sealed class EmailTests : IAsyncLifetime
{
    private readonly EmailSettings _settings = EmailSettings.FromEnvironment();
    private readonly EmailSender _sender;
    private readonly MailHogApiClient _api;

    public EmailTests()
    {
        _sender = new EmailSender(_settings);
        _api = new MailHogApiClient(_settings.ApiBaseUrl);
    }

    public async Task InitializeAsync()
    {
        await _api.WaitUntilReadyAsync(TimeSpan.FromSeconds(30));
        await _api.DeleteAllAsync();
    }

    public Task DisposeAsync() => Task.CompletedTask;

    [Fact]
    public async Task ConfirmacaoDeEmail_EhEnviadaERecebida()
    {
        var token = Guid.NewGuid().ToString("N");
        var to = "novo.usuario@pildra.com.br";
        const string subject = "Confirm your email address";
        var body =
            "Bem-vindo! Confirme seu e-mail acessando o link abaixo:\n" +
            $"https://app.tootega.test/confirm?token={token}\n";

        await _sender.SendAsync(to, subject, body);

        var messages = await _api.WaitForMessagesAsync(1, TimeSpan.FromSeconds(10));

        var received = Assert.Single(messages);
        Assert.Equal(subject, received.Subject);
        Assert.Contains(token, received.Body);
        Assert.Contains("/confirm?token=", received.Body);
    }

    [Fact]
    public async Task ResetDeSenha_EnviaTokenComExpiracao()
    {
        var token = Guid.NewGuid().ToString("N");
        var to = "usuario@pildra.com.br";
        const string subject = "Reset your password";
        var body =
            "Recebemos um pedido de redefinição de senha.\n" +
            $"Token: {token}\n" +
            $"Válido por {_settings.ResetTokenExpiryHours} hora(s).\n" +
            $"https://app.tootega.test/reset-password?token={token}\n";

        await _sender.SendAsync(to, subject, body);

        var messages = await _api.WaitForMessagesAsync(1, TimeSpan.FromSeconds(10));

        var received = Assert.Single(messages);
        Assert.Equal(subject, received.Subject);
        Assert.Contains(token, received.Body);
        Assert.Contains("/reset-password?token=", received.Body);
    }

    [Fact]
    public async Task Api_RetornaMaisNovoPrimeiro()
    {
        // Envia confirmação e, em seguida, o reset. O mais novo (reset) deve
        // vir primeiro na listagem da API.
        await _sender.SendAsync("a@pildra.com.br", "Confirm your email address", "confirmacao");
        await Task.Delay(50); // garante Created distinto
        await _sender.SendAsync("b@pildra.com.br", "Reset your password", "reset");

        var messages = await _api.WaitForMessagesAsync(2, TimeSpan.FromSeconds(10));

        Assert.Equal("Reset your password", messages[0].Subject);
        Assert.Equal("Confirm your email address", messages[1].Subject);
        Assert.True(messages[0].Created >= messages[1].Created);
    }

    [Fact]
    public async Task DestinatarioDeOutroDominio_EhRecusadoENaoApareceNaCaixa()
    {
        await Assert.ThrowsAsync<SmtpCommandException>(
            () => _sender.SendAsync("alguem@gmail.com", "Fora do dominio", "recusado"));

        Assert.Empty(await _api.ListAsync());
    }
}
